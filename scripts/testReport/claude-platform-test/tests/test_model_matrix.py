import re

import config


def test_each_requested_model_has_one_documented_model_case():
    # MODELS 可以只跑其中一部分模型，但每个待测模型都必须有对应的 Case 与唯一编号。
    # case_id 是从 MODELS 自动生成的，这里改为断言其形态而不是写死具体列表。
    assert config.MODELS
    assert set(config.MODELS) <= set(config.MODEL_CASES)
    case_ids = [case["case_id"] for case in config.MODEL_CASES.values()]
    assert len(set(case_ids)) == len(case_ids)
    assert len(case_ids) == len(config.MODELS)
    assert all(re.fullmatch(r"MODEL-\d{3}", case_id) for case_id in case_ids)
    # 顺序与 MODELS 一致
    assert [config.MODEL_CASES[model]["case_id"] for model in config.MODELS] == [
        f"MODEL-{i:03d}" for i in range(1, len(config.MODELS) + 1)
    ]


def test_each_model_case_has_official_capability_expectations():
    required = {
        "family", "thinking", "sampling_parameters", "max_tokens_128001",
        "web_search", "prompt_cache",
    }
    for model, case in config.MODEL_CASES.items():
        assert required <= set(case), model
        assert case["family"] == config.MODEL_METADATA[model]["family"]
        assert case["thinking"] == config.MODEL_METADATA[model]["thinking"]
        assert case["sampling_parameters"] in {"expected_4xx", "supported_without_thinking"}
        assert case["max_tokens_128001"] == "expected_4xx"
        assert case["web_search"] == "test_stream_and_non_stream"
        assert case["prompt_cache"] == {"5m", "1h"}


def test_models_are_grouped_into_named_series():
    assert config.model_series("claude-opus-4-8") == "Opus"
    assert config.model_series("claude-sonnet-5") == "Sonnet"
    assert config.model_series("claude-haiku-4-5-20251001") == "Haiku"
    assert config.model_series("claude-fable-5") == "Fable"
