package operator

import (
	"reflect"
	"testing"
)

func TestSetArg(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		prefix   string
		value    string
		expected []string
	}{
		{
			name:     "replaces an existing arg matching the prefix",
			args:     []string{"--enable-secret-rotation=true", "--rotation-poll-interval=2m"},
			prefix:   "--rotation-poll-interval=",
			value:    "5m0s",
			expected: []string{"--enable-secret-rotation=true", "--rotation-poll-interval=5m0s"},
		},
		{
			name:     "appends the arg when no existing element matches the prefix",
			args:     []string{"--enable-secret-rotation=true"},
			prefix:   "--rotation-poll-interval=",
			value:    "2m0s",
			expected: []string{"--enable-secret-rotation=true", "--rotation-poll-interval=2m0s"},
		},
		{
			name:     "does not reorder or otherwise affect unrelated args",
			args:     []string{"--a=1", "--rotation-poll-interval=2m", "--b=2"},
			prefix:   "--rotation-poll-interval=",
			value:    "10s",
			expected: []string{"--a=1", "--rotation-poll-interval=10s", "--b=2"},
		},
		{
			name:     "appends into an empty args slice",
			args:     []string{},
			prefix:   "--enable-secret-rotation=",
			value:    "false",
			expected: []string{"--enable-secret-rotation=false"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := setArg(tc.args, tc.prefix, tc.value)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("expected args to be %v, got %v", tc.expected, got)
			}
		})
	}
}
