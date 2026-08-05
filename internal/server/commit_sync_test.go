package server

import "testing"

func TestContainsWorkItemIDRequiresACompleteIdentifier(t *testing.T) {
	tests := []struct {
		message string
		want    bool
	}{
		{"finish CORE-1", true},
		{"core-1: finish sync", true},
		{"[CORE-1] finish sync", true},
		{"finish CORE-10", false},
		{"finish XCORE-1", false},
		{"finish CORE-1-extra", false},
	}
	for _, test := range tests {
		if got := containsWorkItemID(test.message, "CORE-1"); got != test.want {
			t.Errorf("containsWorkItemID(%q, CORE-1) = %v, want %v", test.message, got, test.want)
		}
	}
}
