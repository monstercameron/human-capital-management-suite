package demoworkforce

import "fmt"

// shippedHeadshots are the seed headshots the repository carries: person-hc-NNN
// for every N from 1 to 60 that is not a multiple of four. Both demo companies
// draw from this one set (a headshot never appears twice inside a company).
func shippedHeadshots() []int {
	var out []int
	for n := 1; n <= 60; n++ {
		if n%4 != 0 {
			out = append(out, n)
		}
	}
	return out
}

func headshotProxyRef(n int) string {
	return fmt.Sprintf("/workspace/assets/person-hc-%03d-small.jpg", n)
}

// spareHeadshots are the headshots a company chose for people its roster gave
// none, keyed by worker key. The worker rows are append-only and replays check
// the photograph on the stored row, so the roster cannot change; the directory
// lends these instead. Each was picked from the headshots no one in the
// company wears, for the closest fit to the person (three of the four people
// are men and only one of the eleven spare headshots is plainly a man, so the
// other three are the nearest fits rather than exact ones).
var spareHeadshots = map[string]map[string]int{
	IronridgeKey: {
		"ir-014-ben-whitaker":  30,
		"ir-020-chris-yazzie":  54,
		"ir-021-eddie-ramirez": 26,
		"ir-037-hector-salas":  11,
	},
}

// directoryPhotos is the photograph URL of every employee who has one: the
// headshot the plan gives them, else the spare headshot the company chose for
// them, else the first headshot nobody in the company wears. A person left
// over once the headshots run out has no entry and is drawn with initials.
func (p *Pack) directoryPhotos(employees []Employee) map[string]string {
	photos := make(map[string]string, len(employees))
	used := map[int]bool{}
	var bare []string
	for _, employee := range employees {
		key := employee.Row.WorkerKey
		if employee.PhotoProxyRef != "" {
			photos[key] = employee.PhotoProxyRef
			if n := photoNumber(employee.PhotoSourceName); n > 0 {
				used[n] = true
			}
			continue
		}
		if n, ok := spareHeadshots[p.Key][key]; ok {
			photos[key] = headshotProxyRef(n)
			used[n] = true
			continue
		}
		bare = append(bare, key)
	}
	for _, n := range shippedHeadshots() {
		if len(bare) == 0 {
			break
		}
		if used[n] {
			continue
		}
		photos[bare[0]] = headshotProxyRef(n)
		used[n] = true
		bare = bare[1:]
	}
	return photos
}

// photoNumber is N of "hc-NNN.png", or 0 when the name is not one.
func photoNumber(source string) int {
	var n int
	if _, err := fmt.Sscanf(source, "hc-%d.png", &n); err != nil {
		return 0
	}
	return n
}
