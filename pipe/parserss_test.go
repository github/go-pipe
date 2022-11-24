package pipe

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRss(t *testing.T) {
	const kb = 1024

	okExamples := []struct {
		input  string
		result uint64
	}{
		{
			input:  "RssAnon:\t   15032 kB",
			result: 15032 * kb,
		},
		{
			input:  "RssAnon:\t   15032 kB\n",
			result: 15032 * kb,
		},
		{
			input:  "RssAnon:\t99915032 kB",
			result: 99915032 * kb,
		},
		{
			input:  "RssAnon:\t       1 kB",
			result: kb,
		},
	}

	for _, example := range okExamples {
		rss, ok := parseRSS(example.input)
		if assert.Truef(t, ok, "should be able to parse %q", example.input) {
			assert.Equalf(t, example.result, rss, "value of %q", example.input)
		}
	}

	badExamples := []string{
		"",
		"\n",
		"RssAnon:\t 123",
		"RssAnonn:\t 123 kB",
		"RssAno:\t 123 kB",
		"Blah:\t 123 kB",
		"Blah:",
		"123",
	}

	for _, example := range badExamples {
		_, ok := parseRSS(example)
		assert.Falsef(t, ok, "should not be able to parse %q", example)
	}
}

func BenchmarkParseRss(b *testing.B) {
	b.Run("match", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			rss, ok := parseRSS("RssAnon:\t   15032 kB")
			require.True(b, ok)
			require.EqualValues(b, 15032*1024, rss)
		}
	})

	b.Run("no match", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, ok := parseRSS("Other:\t   15032 kB")
			require.False(b, ok)
		}
	})
}
