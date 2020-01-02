package util

import "time"

// TryUntilSuccess will try f until success
func TryUntilSuccess(f func() bool, duration time.Duration) {
	var r bool
	for {
		r = f()
		if r {
			return
		}
		time.Sleep(duration)
	}
}
