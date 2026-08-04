#!/usr/bin/env python3
"""Decode Claude extended-thinking signatures to inspect routing metadata.

Ported from router-for-me/CLIProxyAPI @ 5b7f2361
(internal/signature/claude_validation.go). That project verified this
protobuf tree against real samples from Anthropic Max 20x, Azure, Vertex,
and Bedrock.

Usage:
    python3 claude_signature_decode.py "<signature>"
    python3 claude_signature_decode.py --file sigs.txt   # one signature per line
    python3 claude_signature_decode.py --clipboard       # read from macOS clipboard (pbpaste)
"""

import argparse
import base64
import subprocess
import sys
from dataclasses import dataclass
from typing import Optional


class ProtobufError(ValueError):
    pass


# ---- minimal protobuf wire-format reader (varint + length-delimited only) ----

def _read_varint(buf: bytes, offset: int):
    result = 0
    shift = 0
    while True:
        if offset >= len(buf):
            raise ProtobufError("truncated varint")
        b = buf[offset]
        offset += 1
        result |= (b & 0x7F) << shift
        if not (b & 0x80):
            break
        shift += 7
        if shift >= 64:
            raise ProtobufError("varint too long")
    return result, offset


def _read_tag(buf: bytes, offset: int):
    value, offset = _read_varint(buf, offset)
    field_num = value >> 3
    wire_type = value & 0x7
    return field_num, wire_type, offset


def _read_field_value(buf: bytes, wire_type: int, offset: int):
    """Return (content_bytes, new_offset). content_bytes is the varint value's raw
    bytes for wire_type 0, or the length-delimited payload (length prefix stripped)
    for wire_type 2."""
    if wire_type == 0:  # varint
        start = offset
        _, offset = _read_varint(buf, offset)
        content = buf[start:offset]
    elif wire_type == 1:  # 64-bit fixed
        content = buf[offset:offset + 8]
        offset += 8
    elif wire_type == 2:  # length-delimited
        length, offset = _read_varint(buf, offset)
        content = buf[offset:offset + length]
        offset += length
    elif wire_type == 5:  # 32-bit fixed
        content = buf[offset:offset + 4]
        offset += 4
    else:
        raise ProtobufError(f"unsupported wire type {wire_type}")
    if offset > len(buf):
        raise ProtobufError("field value runs past end of buffer")
    return content, offset


def _walk_fields(buf: bytes):
    """Yield (field_num, wire_type, content_bytes) for each top-level field.

    content_bytes is the varint's own bytes for wire_type 0, or the bytes payload
    (length prefix already stripped) for wire_type 2 -- mirroring protowire.ConsumeBytes.
    """
    offset = 0
    while offset < len(buf):
        field_num, wire_type, offset = _read_tag(buf, offset)
        content, offset = _read_field_value(buf, wire_type, offset)
        yield field_num, wire_type, content


def _decode_varint_value(raw: bytes) -> int:
    value, consumed = _read_varint(raw, 0)
    if consumed != len(raw):
        raise ProtobufError("trailing bytes after varint")
    return value


def _extract_bytes_field(msg: bytes, field_num: int, scope: str) -> bytes:
    value = None
    for num, wire_type, raw in _walk_fields(msg):
        if num != field_num:
            continue
        if wire_type != 2:
            raise ProtobufError(f"{scope} field {field_num} must be length-delimited")
        value = raw
    if value is None:
        raise ProtobufError(f"missing {scope} field {field_num}")
    return value


# ---- Claude signature tree ----

@dataclass
class ClaudeSignatureTree:
    encoding_layers: int
    channel_id: Optional[int] = None
    infra_raw: Optional[int] = None
    model_text: str = ""
    has_field7: bool = False
    routing_class: str = "unknown"
    infrastructure_class: str = "infra_unknown"
    schema_features: str = "unknown_schema_features"
    legacy_route_hint: str = ""


def _strip_cache_prefix(raw_signature: str) -> str:
    sig = raw_signature.strip()
    if "#" in sig:
        sig = sig.split("#", 1)[1].strip()
    return sig


def _inspect_channel_block(channel_block: bytes, encoding_layers: int) -> ClaudeSignatureTree:
    tree = ClaudeSignatureTree(encoding_layers=encoding_layers)
    have_channel_id = False
    has_field6 = False
    has_field7 = False

    for num, wire_type, raw in _walk_fields(channel_block):
        if num == 1:
            if wire_type != 0:
                raise ProtobufError("Field 2.1.1 channel_id must be varint")
            tree.channel_id = _decode_varint_value(raw)
            have_channel_id = True
        elif num == 2:
            if wire_type != 0:
                raise ProtobufError("Field 2.1.2 infra must be varint")
            tree.infra_raw = _decode_varint_value(raw)
        elif num == 6:
            if wire_type != 2:
                raise ProtobufError("Field 2.1.6 model_text must be bytes")
            try:
                tree.model_text = raw.decode("utf-8")
            except UnicodeDecodeError as e:
                raise ProtobufError("Field 2.1.6 model_text is not valid UTF-8") from e
            has_field6 = True
        elif num == 7:
            if wire_type != 0:
                raise ProtobufError("Field 2.1.7 must be varint")
            _decode_varint_value(raw)
            has_field7 = True
            tree.has_field7 = True

    if not have_channel_id:
        raise ProtobufError("missing Field 2.1.1 channel_id")

    if tree.channel_id == 11:
        tree.routing_class = "routing_class_11"
    elif tree.channel_id == 12:
        tree.routing_class = "routing_class_12"

    if tree.infra_raw is None:
        tree.infrastructure_class = "infra_default"
    elif tree.infra_raw == 1:
        tree.infrastructure_class = "infra_aws"
    elif tree.infra_raw == 2:
        tree.infrastructure_class = "infra_google"
    else:
        tree.infrastructure_class = "infra_unknown"

    if has_field6:
        tree.schema_features = "extended_model_tagged_schema"
    elif not has_field6 and not has_field7 and 70 <= len(channel_block) <= 72:
        tree.schema_features = "compact_schema"

    if tree.channel_id == 11:
        if tree.infra_raw is None:
            tree.legacy_route_hint = "legacy_default_group"
        elif tree.infra_raw == 1:
            tree.legacy_route_hint = "legacy_aws_group"
        elif tree.infra_raw == 2 and encoding_layers == 2:
            tree.legacy_route_hint = "legacy_vertex_direct"
        elif tree.infra_raw == 2 and encoding_layers == 1:
            tree.legacy_route_hint = "legacy_vertex_proxy"

    return tree


def inspect_claude_signature_payload(payload: bytes, encoding_layers: int) -> ClaudeSignatureTree:
    if not payload:
        raise ProtobufError("empty payload")
    if payload[0] != 0x12:
        raise ProtobufError(f"expected first byte 0x12, got 0x{payload[0]:02x}")

    container = _extract_bytes_field(payload, 2, "top-level protobuf")
    channel_block = _extract_bytes_field(container, 1, "Claude Field 2 container")
    return _inspect_channel_block(channel_block, encoding_layers)


def decode_claude_thinking_signature(raw_signature: str) -> ClaudeSignatureTree:
    sig = _strip_cache_prefix(raw_signature)
    if not sig:
        raise ValueError("empty signature")

    prefix = sig[0]
    if prefix == "R":
        outer = base64.b64decode(sig, validate=True)
        if not outer or outer[:1] != b"E":
            raise ValueError(f"double-layer signature: inner does not start with 'E', got {outer[:1]!r}")
        payload = base64.b64decode(outer, validate=True)
        encoding_layers = 2
    elif prefix == "E":
        payload = base64.b64decode(sig, validate=True)
        encoding_layers = 1
    else:
        raise ValueError(f"invalid signature: expected 'E' or 'R' prefix, got {prefix!r}")

    return inspect_claude_signature_payload(payload, encoding_layers)


def format_tree(sig: str, tree: ClaudeSignatureTree) -> str:
    lines = [
        f"signature: {sig[:24]}..." if len(sig) > 24 else f"signature: {sig}",
        f"  encoding_layers      = {tree.encoding_layers}",
        f"  channel_id           = {tree.channel_id}",
        f"  infra (raw)          = {tree.infra_raw}",
        f"  model_text           = {tree.model_text!r}",
        f"  has_field7           = {tree.has_field7}",
        f"  routing_class        = {tree.routing_class}",
        f"  infrastructure_class = {tree.infrastructure_class}",
        f"  schema_features      = {tree.schema_features}",
        f"  legacy_route_hint    = {tree.legacy_route_hint or '(n/a, ch != 11)'}",
    ]
    return "\n".join(lines)


def _decode_and_print(sig: str):
    try:
        tree = decode_claude_thinking_signature(sig)
        print(format_tree(sig, tree))
    except (ValueError, ProtobufError) as e:
        print(f"signature: {sig[:24]}...\n  ERROR: {e}")
    print()


def _run_interactive():
    print("粘贴 thinking signature 后回车即可解码，输入空行或 quit 退出。")
    print("（签名很长时终端粘贴容易卡顿/丢字符，建议改用 --clipboard 直接读剪贴板）")
    while True:
        try:
            sig = input("signature> ").strip()
        except (EOFError, KeyboardInterrupt):
            print()
            break
        if not sig or sig.lower() in ("quit", "exit"):
            break
        _decode_and_print(sig)


def _read_clipboard() -> str:
    result = subprocess.run(["pbpaste"], capture_output=True, text=True, check=True)
    return result.stdout.strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("signature", nargs="?", help="raw thinking-block signature string")
    parser.add_argument("--file", help="path to a file with one signature per line")
    parser.add_argument(
        "--clipboard", "-c", action="store_true",
        help="read the signature from the macOS clipboard (pbpaste) instead of typing/pasting it",
    )
    args = parser.parse_args()

    if args.file:
        with open(args.file, "r", encoding="utf-8") as f:
            sigs = [line.strip() for line in f if line.strip()]
        for sig in sigs:
            _decode_and_print(sig)
        return

    if args.clipboard:
        _decode_and_print(_read_clipboard())
        return

    if args.signature:
        _decode_and_print(args.signature)
        return

    _run_interactive()


if __name__ == "__main__":
    main()
