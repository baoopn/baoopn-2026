//go:build race

package mail

// raceEnabled is true under -race, which makes the scale tests 10-20x slower.
const raceEnabled = true
