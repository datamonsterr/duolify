package characters_test

import "os"

func readSource(path string) (string, error) {
	raw, err := os.ReadFile(path)
	return string(raw), err
}
