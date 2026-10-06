// Command demo shows the intervalres API with a few built-in intervals.
package main

import (
	"errors"
	"fmt"

	"intervalres"
)

func main() {
	m := intervalres.NewManager()

	fmt.Println("== Reserve ==")
	report("Reserve(alpha, 0, 10)", m.Reserve("alpha", 0, 10))
	report("Reserve(beta, 10, 20)", m.Reserve("beta", 10, 20))
	report("Reserve(gamma, 30, 40)", m.Reserve("gamma", 30, 40))

	fmt.Println("== Overlap rejected ==")
	report("Reserve(delta, 5, 15)", m.Reserve("delta", 5, 15))

	fmt.Println("== Replace ==")
	report("Replace(gamma, 20, 30)", m.Replace("gamma", 20, 30))
	report("Replace(gamma, 8, 12)", m.Replace("gamma", 8, 12))

	fmt.Println("== Find ==")
	if r, ok := m.Find(25); ok {
		fmt.Printf("Find(25)  -> %q [%d, %d)\n", r.ID, r.Start, r.End)
	}
	if _, ok := m.Find(50); !ok {
		fmt.Println("Find(50)  -> no reservation")
	}

	fmt.Println("== Release ==")
	report("Release(alpha)", m.Release("alpha"))

	fmt.Println("== Snapshot ==")
	for _, r := range m.Snapshot() {
		fmt.Printf("  %q [%d, %d)\n", r.ID, r.Start, r.End)
	}
	fmt.Println("Len:", m.Len())
}

func report(op string, err error) {
	var oe *intervalres.OverlapError
	switch {
	case err == nil:
		fmt.Println(op, "-> ok")
	case errors.As(err, &oe):
		fmt.Printf("%s -> rejected: overlaps %q [%d, %d)\n",
			op, oe.Conflict.ID, oe.Conflict.Start, oe.Conflict.End)
	default:
		fmt.Println(op, "->", err)
	}
}
