package pipe

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopyEnvWithOverride(t *testing.T) {
	examples := []struct {
		label          string
		env            []string
		overrides      map[string]string
		expectedResult []string
	}{
		{
			label:     "empty",
			overrides: map[string]string{}, // can't be nil
		},
		{
			label: "original env only",
			env: []string{
				"A=B",
				"B=C",
			},
			overrides: map[string]string{},
			expectedResult: []string{
				"A=B",
				"B=C",
			},
		},
		{
			label: "overrides only",
			env:   []string{},
			overrides: map[string]string{
				"A": "B",
				"B": "C",
			},
			expectedResult: []string{
				"A=B",
				"B=C",
			},
		},
		{
			label: "mix of overrides and not",
			env: []string{
				"ORIGINAL1=abc",
				"ORIGINAL2=def",
			},
			overrides: map[string]string{
				"ORIGINAL1": "override1",
				"OVERRIDE1": "also override",
			},
			expectedResult: []string{
				"ORIGINAL1=override1",
				"ORIGINAL2=def",
				"OVERRIDE1=also override",
			},
		},
		{
			label: "random equal signs",
			env: []string{
				"ORIGINAL1=abc=foo",
				"ORIGINAL2=def=bar",
			},
			overrides: map[string]string{
				"ORIGINAL1": "new=new-new",
				"OVERRIDE1": "also=new-new",
			},
			expectedResult: []string{
				"ORIGINAL1=new=new-new",
				"ORIGINAL2=def=bar",
				"OVERRIDE1=also=new-new",
			},
		},
	}

	for _, ex := range examples {
		ex := ex
		t.Run(ex.label, func(t *testing.T) {
			assert.ElementsMatch(t, ex.expectedResult,
				copyEnvWithOverrides(ex.env, ex.overrides))
		})
	}
}
