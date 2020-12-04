package main

import "bytes"

type CommaWords []string

var commaBytes = []byte(",")

func (w *CommaWords) UnmarshalText(text []byte) error {
	words := bytes.Split(text, commaBytes)

	for _, word := range words {
		*w = append(*w, string(bytes.TrimSpace(word)))
	}

	return nil
}

func (w CommaWords) Include(word string) bool {
	for _, cw := range w {
		if cw == word {
			return true
		}
	}
	return false
}
