import config
from cases import ALL_CASES
from cases.parameters import CASES, EXPECTED_ERROR_CASES


def test_requested_models_are_grouped_by_family_and_thinking_mode():
    # MODELS 可按需增删；MODEL_METADATA 是从 MODELS 自动生成的，
    # 所以待测模型必然都在里面查得到——这里改为验证推导/覆盖机制本身。
    assert config.MODELS
    assert set(config.MODELS) <= set(config.MODEL_METADATA)

    # family 靠 _derive_family 从模型名推断
    assert config._derive_family("claude-opus-4-8") == "opus"
    assert config._derive_family("claude-sonnet-5") == "sonnet"
    assert config._derive_family("claude-fable-5-1") == "fable"  # 不被 "-5-1" 干扰

    # 写了 override 的模型：覆盖值生效
    assert config.MODEL_METADATA["claude-haiku-4-5-20251001"]["thinking"] == "manual"

    # 没写 override 的模型：默认 adaptive
    assert config.MODEL_METADATA["claude-opus-4-8"]["thinking"] == "adaptive"
    assert config.MODEL_METADATA["claude-opus-5"]["thinking"] == "adaptive"


def test_parameter_cases_have_explicit_model_dependent_contract():
    assert [case.case_id for case in CASES] == [
        "AN-PARAM-001",
        "AN-PARAM-002",
        "AN-PARAM-003",
        "AN-PARAM-004",
    ]
    assert [case.case_id for case in EXPECTED_ERROR_CASES] == [case.case_id for case in CASES]
    assert {case.category for case in CASES} == {"参数边界"}
    assert {case.case_id for case in ALL_CASES} >= {case.case_id for case in CASES}
    assert not any(case.case_id.startswith("AN-STRESS-") for case in ALL_CASES)


def test_parameter_case_metadata_names_the_parameter_and_model_dependent_expectation():
    for case in CASES:
        if case.parameter_name == "max_tokens":
            assert case.expected_error is True
            assert case.expected_status == "4xx"
        else:
            assert case.expected_error is False
            assert case.expected_status == "model-dependent"
        assert case.parameter_name in {"temperature", "top_p", "top_k", "max_tokens"}
