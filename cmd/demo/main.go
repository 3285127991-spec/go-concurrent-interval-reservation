// Command demo is a minimal walkthrough of the intervalreservation library.
package main

import (
	"fmt"
	"log"

	"intervalreservation"
)

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	m := intervalreservation.NewManager()

	// Reserve two adjacent intervals; touching endpoints are allowed.
	must(m.Reserve(intervalreservation.Reservation{ID: "alpha", Start: 0, End: 10}))
	must(m.Reserve(intervalreservation.Reservation{ID: "beta", Start: 10, End: 20}))
	fmt.Println("reserved alpha [0,10) and beta [10,20); Len =", m.Len())

	// Overlapping reservations are rejected.
	err := m.Reserve(intervalreservation.Reservation{ID: "gamma", Start: 5, End: 15})
	fmt.Println("reserve gamma [5,15) rejected:", err)

	// Replace atomically moves an existing reservation.
	must(m.Replace("beta", 20, 30))
	r, _ := m.Get("beta")
	fmt.Printf("replaced beta -> [%d,%d)\n", r.Start, r.End)

	// Find locates the reservation containing a point.
	if r, ok := m.Find(25); ok {
		fmt.Printf("Find(25) = %q [%d,%d)\n", r.ID, r.Start, r.End)
	}
	if _, ok := m.Find(15); !ok {
		fmt.Println("Find(15) = no reservation")
	}

	// Release removes a reservation by ID.
	must(m.Release("alpha"))
	fmt.Println("released alpha; Len =", m.Len())

	// Snapshot lists remaining reservations in sorted order.
	fmt.Println("Snapshot:")
	for _, r := range m.Snapshot() {
		fmt.Printf("  %q [%d,%d)\n", r.ID, r.Start, r.End)
	}
}
