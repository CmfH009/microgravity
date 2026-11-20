package verifier

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyGoProject(t *testing.T) {
	tempDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module testmod\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	testFile := filepath.Join(tempDir, "sample_test.go")
	testCode := `package testmod

import "testing"

func TestSample(t *testing.T) {
	if 1+1 != 2 {
		t.Errorf("Math broken")
	}
}
`
	if err := os.WriteFile(testFile, []byte(testCode), 0644); err != nil {
		t.Fatalf("Failed to create sample_test.go: %v", err)
	}

	v := New()
	res, err := v.Verify(context.Background(), tempDir)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if !res.Passed {
		t.Errorf("Expected verification to pass, output: %s", res.Output)
	}

	if !strings.Contains(res.Command, "go test") {
		t.Errorf("Expected command to contain 'go test', got '%s'", res.Command)
	}

	badge := res.RenderBadge()
	if badge == "" || !strings.Contains(badge, "VERIFICATION PASSED") {
		t.Errorf("Expected badge to contain 'VERIFICATION PASSED', got: %s", badge)
	}
}

func TestVerifyFailingGoProject(t *testing.T) {
	tempDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module testmod\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	testFile := filepath.Join(tempDir, "failing_test.go")
	testCode := `package testmod

import "testing"

func TestFail(t *testing.T) {
	t.Fatalf("Intentional failure")
}
`
	if err := os.WriteFile(testFile, []byte(testCode), 0644); err != nil {
		t.Fatalf("Failed to create failing_test.go: %v", err)
	}

	v := New()
	res, err := v.Verify(context.Background(), tempDir)
	if err != nil {
		t.Fatalf("Verify returned unexpected error: %v", err)
	}

	if res.Passed {
		t.Errorf("Expected verification to fail")
	}

	badge := res.RenderBadge()
	if badge == "" || !strings.Contains(badge, "VERIFICATION FAILED") {
		t.Errorf("Expected badge to contain 'VERIFICATION FAILED', got: %s", badge)
	}
}

func TestDetectFrameworks(t *testing.T) {
	tempDir := t.TempDir()

	_ = os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(`{"name":"test","devDependencies":{"vitest":"^1.0.0"}}`), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "pytest.ini"), []byte("[pytest]\n"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "Cargo.toml"), []byte("[package]\nname=\"test_cargo\"\nversion=\"0.1.0\"\n"), 0644)

	v := New()
	frameworks := v.DetectFrameworks(tempDir)

	if len(frameworks) != 3 {
		t.Fatalf("Expected 3 frameworks detected, got %d", len(frameworks))
	}

	types := map[FrameworkType]bool{}
	for _, fw := range frameworks {
		types[fw.Type] = true
	}

	if !types[FrameworkVitest] {
		t.Errorf("Expected FrameworkVitest to be detected")
	}
	if !types[FrameworkPytest] {
		t.Errorf("Expected FrameworkPytest to be detected")
	}
	if !types[FrameworkCargo] {
		t.Errorf("Expected FrameworkCargo to be detected")
	}
}

func TestDiagnoseOutputGo(t *testing.T) {
	goOutput := `
--- FAIL: TestMath (0.01s)
    math_test.go:24: Expected 4, got 5
FAIL
FAIL	github.com/example/math	0.012s
`
	diags := DiagnoseOutput(goOutput, FrameworkGo)
	if len(diags) == 0 {
		t.Fatalf("Expected diagnoses for Go output, got 0")
	}
	if diags[0].TestName != "TestMath" {
		t.Errorf("Expected TestName 'TestMath', got '%s'", diags[0].TestName)
	}
	if diags[0].FilePath != "math_test.go" {
		t.Errorf("Expected FilePath 'math_test.go', got '%s'", diags[0].FilePath)
	}
	if diags[0].LineNumber != 24 {
		t.Errorf("Expected LineNumber 24, got %d", diags[0].LineNumber)
	}
}

func TestDiagnoseOutputJS(t *testing.T) {
	jsOutput := `
FAIL src/calculator.test.ts
  ✕ adds two numbers
    AssertionError: expected 3 to equal 4
      at src/calculator.test.ts:18:9
`
	diags := DiagnoseOutput(jsOutput, FrameworkVitest)
	if len(diags) == 0 {
		t.Fatalf("Expected diagnoses for JS output, got 0")
	}
	if diags[0].FilePath != "src/calculator.test.ts" {
		t.Errorf("Expected FilePath 'src/calculator.test.ts', got '%s'", diags[0].FilePath)
	}
	if diags[0].LineNumber != 18 {
		t.Errorf("Expected LineNumber 18, got %d", diags[0].LineNumber)
	}
}

func TestDiagnoseOutputPytest(t *testing.T) {
	pyOutput := `
FAILED tests/test_str.py::test_concat - AssertionError: assert 'ab' == 'abc'
File "tests/test_str.py", line 12, in test_concat
E   AssertionError: assert 'ab' == 'abc'
`
	diags := DiagnoseOutput(pyOutput, FrameworkPytest)
	if len(diags) == 0 {
		t.Fatalf("Expected diagnoses for Pytest output, got 0")
	}
	if diags[0].FilePath != "tests/test_str.py" {
		t.Errorf("Expected FilePath 'tests/test_str.py', got '%s'", diags[0].FilePath)
	}
	if diags[0].LineNumber != 12 {
		t.Errorf("Expected LineNumber 12, got %d", diags[0].LineNumber)
	}
}

func TestDiagnoseOutputCargo(t *testing.T) {
	cargoOutput := `
running 1 test
test tests::test_add ... FAILED

failures:

---- tests::test_add stdout ----
thread 'tests::test_add' panicked at 'assertion failed: (left == right)', src/lib.rs:25:9
`
	diags := DiagnoseOutput(cargoOutput, FrameworkCargo)
	if len(diags) == 0 {
		t.Fatalf("Expected diagnoses for Cargo output, got 0")
	}
	if diags[0].TestName != "tests::test_add" {
		t.Errorf("Expected TestName 'tests::test_add', got '%s'", diags[0].TestName)
	}
	if diags[0].FilePath != "src/lib.rs" {
		t.Errorf("Expected FilePath 'src/lib.rs', got '%s'", diags[0].FilePath)
	}
	if diags[0].LineNumber != 25 {
		t.Errorf("Expected LineNumber 25, got %d", diags[0].LineNumber)
	}
}

func TestAutoFixFeedbackLoopGo(t *testing.T) {
	tempDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module autofixtest\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	testFile := filepath.Join(tempDir, "calc_test.go")
	testCode := `package autofixtest

import "testing"

func TestAdd(t *testing.T) {
	got := 2 + 2
	want := 5
	if got != want {
		t.Errorf("got %d, expected %d", got, want)
	}
}
`
	if err := os.WriteFile(testFile, []byte(testCode), 0644); err != nil {
		t.Fatalf("Failed to write calc_test.go: %v", err)
	}

	v := New()
	res, err := v.AutoFix(context.Background(), tempDir, FixOptions{
		MaxAttempts: 3,
		AutoCommit:  false,
	})

	if err != nil {
		t.Fatalf("AutoFix returned unexpected error: %v", err)
	}

	if !res.Passed {
		t.Errorf("Expected AutoFix feedback loop to pass cleanly after heuristic repair, output: %s", res.Output)
	}
}

func TestAutoFixFeedbackLoopPytest(t *testing.T) {
	hasPytest := exec.Command("pytest", "--version").Run() == nil
	hasPythonPytest := exec.Command("python", "-m", "pytest", "--version").Run() == nil
	if !hasPytest && !hasPythonPytest {
		t.Skip("Pytest is not available in environment")
	}

	tempDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tempDir, "pytest.ini"), []byte("[pytest]\n"), 0644)
	testFile := filepath.Join(tempDir, "test_calc.py")
	testCode := `def test_add():
    got = 2 + 2
    want = 5
    assert got == want
`
	_ = os.WriteFile(testFile, []byte(testCode), 0644)

	v := New()
	res, err := v.AutoFix(context.Background(), tempDir, FixOptions{
		MaxAttempts: 3,
		AutoCommit:  false,
	})

	if err != nil {
		t.Fatalf("Pytest AutoFix returned error: %v", err)
	}
	if !res.Passed {
		t.Errorf("Expected Pytest AutoFix loop to pass, output: %s", res.Output)
	}
}

func TestDiagnoseOutputPytest_TbAutoWithWarnings(t *testing.T) {
	pyOutput := `C:\Python314\Lib\site-packages\pytest_asyncio\plugin.py:300: PytestDeprecationWarning: The configuration option "asyncio_default_fixture_loop_scope" is unset.
  warnings.warn(PytestDeprecationWarning(_DEFAULT_FIXTURE_LOOP_SCOPE_UNSET))
============================= test session starts =============================
platform win32 -- Python 3.14.6, pytest-9.2.0, pluggy-1.6.0
cachedir: .pytest_cache
rootdir: C:\Temp\proj
configfile: pytest.ini
plugins: anyio-4.14.2, jaxtyping-0.3.11, asyncio-1.4.1, cov-7.1.0
collected 1 item

test_calc.py F                                                           [100%]

================================== FAILURES ===================================
__________________________________ test_add ___________________________________

    def test_add():
        got = 2 + 2
        want = 5
>       assert got == want
E       assert 4 == 5

test_calc.py:4: AssertionError
============================== warnings summary ===============================
..\..\..\..\..\..\Python314\Lib\site-packages\anyio\pytest_plugin.py:14
  C:\Python314\Lib\site-packages\anyio\pytest_plugin.py:14: PytestRemovedIn10Warning: _pytest.python.CallSpec2 has been renamed to CallSpec.
    from _pytest.python import CallSpec2

-- Docs: https://docs.pytest.org/en/stable/how-to/capture-warnings.html
=========================== short test summary info ===========================
FAILED test_calc.py::test_add - assert 4 == 5
======================== 1 failed, 1 warning in 0.17s =========================`

	diags := DiagnoseOutput(pyOutput, FrameworkPytest)
	if len(diags) != 1 {
		t.Fatalf("Expected exactly 1 diagnosis, got %d: %+v", len(diags), diags)
	}
	if diags[0].FilePath != "test_calc.py" {
		t.Errorf("Expected FilePath 'test_calc.py', got %q", diags[0].FilePath)
	}
	if diags[0].LineNumber != 4 {
		t.Errorf("Expected LineNumber 4, got %d", diags[0].LineNumber)
	}
	if diags[0].TestName != "test_add" {
		t.Errorf("Expected TestName 'test_add', got %q", diags[0].TestName)
	}
	if !strings.Contains(diags[0].Message, "assert 4 == 5") {
		t.Errorf("Expected Message to contain 'assert 4 == 5', got %q", diags[0].Message)
	}
}

func TestDiagnoseOutputPytest_MultipleFailuresAndSummaryFallback(t *testing.T) {
	pyOutput := strings.ReplaceAll(`================================== FAILURES ===================================
__________________________________ test_one ___________________________________

    def test_one():
>       assert 1 == 2
E       assert 1 == 2

tests/test_one.py:2: AssertionError
__________________________________ test_two ___________________________________

    def test_two():
>       assert 'a' == 'b'
E       AssertionError: assert 'a' == 'b'
E         - b
E         + a

tests/test_two.py:7: AssertionError
=========================== short test summary info ===========================
FAILED tests/test_one.py::test_one - assert 1 == 2
FAILED tests/test_two.py::test_two - AssertionError: assert 'a' == 'b'
FAILED tests/test_three.py::test_three - ValueError: missing key
`, "\n", "\r\n")

	diags := DiagnoseOutput(pyOutput, FrameworkPytest)
	if len(diags) != 3 {
		t.Fatalf("Expected 3 diagnoses, got %d: %+v", len(diags), diags)
	}

	if diags[0].FilePath != "tests/test_one.py" || diags[0].LineNumber != 2 || diags[0].TestName != "test_one" || !strings.Contains(diags[0].Message, "assert 1 == 2") {
		t.Errorf("Unexpected first diagnosis: %+v", diags[0])
	}
	if diags[1].FilePath != "tests/test_two.py" || diags[1].LineNumber != 7 || diags[1].TestName != "test_two" || !strings.Contains(diags[1].Message, "assert 'a' == 'b'") {
		t.Errorf("Unexpected second diagnosis: %+v", diags[1])
	}
	if diags[2].FilePath != "tests/test_three.py" || diags[2].TestName != "test_three" || diags[2].Message != "ValueError: missing key" {
		t.Errorf("Unexpected third (summary fallback) diagnosis: %+v", diags[2])
	}
}

func TestDiagnoseOutputPytest_SummaryOnly(t *testing.T) {
	pyOutput := `=========================== short test summary info ===========================
FAILED tests/test_api.py::test_status - AssertionError: 500 != 200`

	diags := DiagnoseOutput(pyOutput, FrameworkPytest)
	if len(diags) != 1 {
		t.Fatalf("Expected 1 diagnosis, got %d: %+v", len(diags), diags)
	}
	if diags[0].FilePath != "tests/test_api.py" {
		t.Errorf("Expected FilePath 'tests/test_api.py', got %q", diags[0].FilePath)
	}
	if diags[0].TestName != "test_status" {
		t.Errorf("Expected TestName 'test_status', got %q", diags[0].TestName)
	}
	if diags[0].Message != "AssertionError: 500 != 200" {
		t.Errorf("Expected Message 'AssertionError: 500 != 200', got %q", diags[0].Message)
	}
}

func TestDiagnoseOutputPytest_TbShort(t *testing.T) {
	pyOutput := `================================== FAILURES ===================================
__________________________________ test_mul ___________________________________
test_short.py:9: in test_mul
    assert 2 * 3 == 7
E   assert 6 == 7
=========================== short test summary info ===========================
FAILED test_short.py::test_mul - assert 6 == 7`

	diags := DiagnoseOutput(pyOutput, FrameworkPytest)
	if len(diags) != 1 {
		t.Fatalf("Expected 1 diagnosis, got %d: %+v", len(diags), diags)
	}
	if diags[0].FilePath != "test_short.py" || diags[0].LineNumber != 9 || diags[0].TestName != "test_mul" || !strings.Contains(diags[0].Message, "assert 6 == 7") {
		t.Errorf("Unexpected diagnosis for --tb=short: %+v", diags[0])
	}
}
