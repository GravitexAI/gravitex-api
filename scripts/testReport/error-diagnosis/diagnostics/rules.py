"""Deterministic text classification only; no AI suggestions or network calls."""
import re
from functools import lru_cache

RULE_VERSION = "2.0"
RULES = [
    ('访问被禁用', re.compile('access.{0,25}disabled|account.{0,25}(?:disabled|suspended)|访问.{0,8}禁用', re.I | re.S)),
    ('内容或版权限制', re.compile('sensitivecontent|privacyinformation|copyright|safety system|content.{0,12}(?:policy|filter)|policyviolation|may contain (?:sensitive|real person)|内容审核|版权|隐私', re.I | re.S)),
    ('思考签名或会话上下文', re.compile('invalid.{0,20}signature|thinking.{0,120}(?:cannot be modified|must remain)|encrypted content|signature.{0,50}thinking', re.I | re.S)),
    ('Token或上下文超限', re.compile('prompt.{0,20}too long|context.{0,20}(?:length|limit|exceed)|token count exceeds|maximum.{0,15}context|too many tokens', re.I | re.S)),
    ('图片大小或尺寸超限', re.compile('image.{0,40}(?:exceeds|too large|size limit)|image.{0,20}maximum.{0,15}(?:mb|bytes)|(?:width|height|pixel count|aspect ratio).{0,80}(?:at most|at least|must be|expected)|expected.{0,30}(?:width|height|aspect ratio)', re.I | re.S)),
    ('图片格式或处理异常', re.compile('unsupportedimageformat|invalid image format|could not process image|decode image|base64 image|image format.{0,20}not supported', re.I | re.S)),
    ('工具或Schema不兼容', re.compile('input_schema|json schema|tool_use|tool_result|tool type|tools\\.?\\[(?:\\d|N)|tool_choice|function.{0,60}schema|tool.{0,30}not supported', re.I | re.S)),
    ('请求体过大', re.compile('status_code[=: ]+413|request (?:body|entity).{0,20}(?:too large|large)|payload too large', re.I | re.S)),
    ('限流或配额不足', re.compile('status_code[=: ]+429|rate.?limit|quota|too many requests|resource.?exhausted|限流|配额', re.I | re.S)),
    ('认证或权限异常', re.compile('status_code[=: ]+(?:401|403)|unauthorized|authentication|invalid api.?key|permission.?denied', re.I | re.S)),
    ('余额或计费异常', re.compile('insufficient.{0,20}(?:balance|credit)|billing|余额不足|欠费', re.I | re.S)),
    ('超时或素材获取异常', re.compile('timeout|timed out|cannot fetch|url_unreachable|resource download failed|status_code[=: ]+(?:408|504|524)|超时', re.I | re.S)),
    ('上游服务异常', re.compile('status_code[=: ]+5\\d\\d|overloaded|no available channel|no channels available|internal server|bad gateway|服务不可用', re.I | re.S)),
    ('请求参数或能力不兼容', re.compile('invalidparameter|invalid.argument|validationexception|not supported|unsupported|must be|cannot|not allowed|status_code[=: ]+400', re.I | re.S)),
]

@lru_cache(maxsize=2048)
def classify(text):
    for category, pattern in RULES:
        if pattern.search(text):
            return category
    return "其他待核查"
