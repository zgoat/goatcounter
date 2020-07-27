package goatcounter

import "testing"

func TestXXXXX(t *testing.T) {
	t.Error("XXXXXX")
}

func TestRace(t *testing.T) {
	var i int
	go func() { i++ }()
	go func() { i++ }()
	go func() { i++ }()
	i++
}
