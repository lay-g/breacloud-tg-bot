package humanize

import "testing"

func TestBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{16158429527, "15.05 GB"},
		{30944777449, "28.82 GB"},
		{-1, "0 B"},
	}
	for _, tc := range cases {
		if got := Bytes(tc.in); got != tc.want {
			t.Errorf("Bytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMemory(t *testing.T) {
	if got := Memory(1024); got != "1 GB" {
		t.Errorf("Memory(1024) = %q", got)
	}
	if got := Memory(512); got != "512 MB" {
		t.Errorf("Memory(512) = %q", got)
	}
}

func TestDateAndDateTime(t *testing.T) {
	if got := Date("2027-08-23T00:00:00Z"); got != "2027-08-23" {
		t.Errorf("Date = %q", got)
	}
	if got := Date(""); got != "-" {
		t.Errorf("Date(空) = %q", got)
	}
	if got := DateTime("2026-09-13T06:30:00Z"); got != "09-13 06:30" {
		t.Errorf("DateTime = %q", got)
	}
	if got := DateTime(""); got != "-" {
		t.Errorf("DateTime(空) = %q", got)
	}
}

func TestOnOff(t *testing.T) {
	if OnOff(true) != "已开启" || OnOff(false) != "已关闭" {
		t.Error("OnOff 输出不符预期")
	}
}
