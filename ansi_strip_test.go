package main

import (
	"math/rand"
	"strings"
	"testing"
)

func TestStripANSIEquivalent(t *testing.T) {
	cases := []string{"", "Plain 😀 ▀ text", "\x1b[m", "\x1b[38;2;255;0;0mRed\x1b[0m", "\x1b[2Jkeep", "\x1b[38:2mkeep", "\x1b[;m", "\x1b[\x1b[1m", "\x1b", "\x1b[", "\xff\x1b[0m\xfe"}
	rng := rand.New(rand.NewSource(102))
	alphabet := []byte("abcXYZ0123456789;m[\x1b\x00\xff\n")
	for i := 0; i < 10000; i++ {
		value := make([]byte, rng.Intn(256))
		for j := range value {
			value[j] = alphabet[rng.Intn(len(alphabet))]
		}
		cases = append(cases, string(value))
	}
	for _, input := range cases {
		if got, want := stripANSI(input), ansiPattern.ReplaceAllString(input, ""); got != want {
			t.Fatalf("input %q: got %q, want %q", input, got, want)
		}
	}
}

func BenchmarkStripANSI(b *testing.B) {
	for name, text := range map[string]string{"plain": strings.Repeat("█ ▀ text ", 30), "colour": strings.Repeat("\x1b[38;2;255;85;0m█\x1b[0m ", 30)} {
		b.Run(name, func(b *testing.B) {
			for name, strip := range map[string]func(string) string{"scan": stripANSI, "regexp": func(s string) string { return ansiPattern.ReplaceAllString(s, "") }} {
				b.Run(name, func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						if strip(text) == "" {
							b.Fatal("empty")
						}
					}
				})
			}
		})
	}
}
