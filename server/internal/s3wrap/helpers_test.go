package s3wrap

import (
	"testing"
)

// TestOptionalString 空串映射为 nil（SDK 省略参数），非空返回指针。
func TestOptionalString(t *testing.T) {
	if got := optionalString(""); got != nil {
		t.Fatalf("optionalString(\"\") = %v, want nil", got)
	}
	p := optionalString("x")
	if p == nil || *p != "x" {
		t.Fatalf("optionalString(\"x\") = %v, want &x", p)
	}
}

// TestDerefString nil 指针解引用为空串。
func TestDerefString(t *testing.T) {
	if got := derefString(nil); got != "" {
		t.Fatalf("derefString(nil) = %q, want empty", got)
	}
	v := "hello"
	if got := derefString(&v); got != "hello" {
		t.Fatalf("derefString = %q, want hello", got)
	}
}

// TestDerefInt32 nil 指针解引用为 0。
func TestDerefInt32(t *testing.T) {
	if got := derefInt32(nil); got != 0 {
		t.Fatalf("derefInt32(nil) = %d, want 0", got)
	}
	v := int32(7)
	if got := derefInt32(&v); got != 7 {
		t.Fatalf("derefInt32 = %d, want 7", got)
	}
}

// TestBoolOrFalse nil 指针按 false 处理。
func TestBoolOrFalse(t *testing.T) {
	if boolOrFalse(nil) {
		t.Fatal("nil should be false")
	}
	tr := true
	if !boolOrFalse(&tr) {
		t.Fatal("true pointer should stay true")
	}
	fa := false
	if boolOrFalse(&fa) {
		t.Fatal("false pointer should stay false")
	}
}
