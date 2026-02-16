package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"rooms/solver"
)

type tripData struct {
	RoomSize          int `json:"room_size"`
	PreferNotMultiple int `json:"prefer_not_multiple"`
	NoPreferCost      int `json:"no_prefer_cost"`
}

type studentData struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type constraintsData struct {
	Overalls []struct {
		StudentAID int64  `json:"student_a_id"`
		StudentBID int64  `json:"student_b_id"`
		Kind       string `json:"kind"`
	} `json:"overalls"`
}

func normalizeKey(a []int) string {
	rm := map[int][]int{}
	for i, room := range a {
		rm[room] = append(rm[room], i)
	}
	var gs [][]int
	for _, members := range rm {
		slices.Sort(members)
		gs = append(gs, members)
	}
	slices.SortFunc(gs, func(a, b []int) int { return a[0] - b[0] })
	var buf strings.Builder
	for _, g := range gs {
		for i, m := range g {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.WriteString(strconv.Itoa(m))
		}
		buf.WriteByte(';')
	}
	return buf.String()
}

type runResult struct {
	score     int
	solutions [][]int
	elapsed   time.Duration
}

func printStats(label string, results []runResult, runs int) {
	scores := map[int]int{}
	solutionSets := map[string]int{}
	var totalTime time.Duration
	var totalSolutions int

	for _, r := range results {
		totalTime += r.elapsed
		scores[r.score]++
		totalSolutions += len(r.solutions)
		for _, sol := range r.solutions {
			key := normalizeKey(sol)
			solutionSets[key]++
		}
	}

	fmt.Printf("--- %s ---\n", label)
	fmt.Printf("  avg time: %v\n", totalTime/time.Duration(runs))

	var scoreList []struct {
		score int
		count int
	}
	for s, c := range scores {
		scoreList = append(scoreList, struct {
			score int
			count int
		}{s, c})
	}
	sort.Slice(scoreList, func(i, j int) bool { return scoreList[i].score > scoreList[j].score })

	fmt.Printf("  score distribution:\n")
	for _, sc := range scoreList {
		fmt.Printf("    score %d: %d/%d runs (%.0f%%)\n", sc.score, sc.count, runs, float64(sc.count)/float64(runs)*100)
	}

	fmt.Printf("  unique solutions seen: %d\n", len(solutionSets))
	fmt.Printf("  avg solutions per run: %.1f\n", float64(totalSolutions)/float64(runs))

	var solFreqs []struct {
		key   string
		count int
	}
	for k, c := range solutionSets {
		solFreqs = append(solFreqs, struct {
			key   string
			count int
		}{k, c})
	}
	sort.Slice(solFreqs, func(i, j int) bool { return solFreqs[i].count > solFreqs[j].count })

	stableCount := 0
	for _, sf := range solFreqs {
		if sf.count == runs {
			stableCount++
		}
	}
	fmt.Printf("  solutions found in all runs: %d\n", stableCount)
	if len(solFreqs) > 0 {
		topN := min(5, len(solFreqs))
		fmt.Printf("  top %d solution frequencies: ", topN)
		for i := range topN {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%d/%d", solFreqs[i].count, runs)
		}
		fmt.Println()
	}
	fmt.Println()
}

func main() {
	dir := flag.String("dir", "tmp", "directory with trip/students/constraints JSON files")
	runs := flag.Int("runs", 20, "number of solver runs per parameter set")
	algo := flag.String("algo", "both", "algorithm: hillclimb, fast, sa, hybrid, both, all")
	numRandom := flag.String("random", "30", "comma-separated random placement counts (hillclimb)")
	numPerturb := flag.String("perturb", "200", "comma-separated perturbation counts (hillclimb)")
	perturbMin := flag.Int("pmin", 2, "perturbation min groups (hillclimb)")
	perturbMax := flag.Int("pmax", 5, "perturbation max groups (hillclimb)")
	saRestarts := flag.String("restarts", "20", "comma-separated SA/hybrid restart counts")
	saSteps := flag.String("steps", "10000", "comma-separated SA step counts")
	hybridSteps := flag.String("hsteps", "5000", "comma-separated hybrid SA step counts")
	saTempHigh := flag.Float64("thigh", 5.0, "SA initial temperature")
	saTempLow := flag.Float64("tlow", 0.01, "SA final temperature")
	hybridTempHigh := flag.Float64("hthigh", 10.0, "hybrid SA initial temperature")
	hybridTempLow := flag.Float64("htlow", 0.1, "hybrid SA final temperature")
	flag.Parse()

	tripBytes, err := os.ReadFile(*dir + "/1")
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading trip: %v\n", err)
		os.Exit(1)
	}
	var trip tripData
	json.Unmarshal(tripBytes, &trip)

	studentsBytes, err := os.ReadFile(*dir + "/students")
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading students: %v\n", err)
		os.Exit(1)
	}
	var students []studentData
	json.Unmarshal(studentsBytes, &students)

	constraintsBytes, err := os.ReadFile(*dir + "/constraints")
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading constraints: %v\n", err)
		os.Exit(1)
	}
	var cd constraintsData
	json.Unmarshal(constraintsBytes, &cd)

	idx := map[int64]int{}
	for i, s := range students {
		idx[s.ID] = i
	}
	n := len(students)

	var constraints []solver.Constraint
	for _, o := range cd.Overalls {
		ai, aOk := idx[o.StudentAID]
		bi, bOk := idx[o.StudentBID]
		if !aOk || !bOk {
			continue
		}
		constraints = append(constraints, solver.Constraint{
			StudentA: ai,
			StudentB: bi,
			Kind:     o.Kind,
		})
	}

	fmt.Printf("Students: %d, Room size: %d, Constraints: %d\n", n, trip.RoomSize, len(constraints))
	fmt.Printf("Prefer Not multiple: %d, No Prefer cost: %d\n", trip.PreferNotMultiple, trip.NoPreferCost)
	fmt.Printf("Runs per config: %d\n\n", *runs)

	if *algo == "hillclimb" || *algo == "both" {
		randomCounts := parseIntList(*numRandom)
		perturbCounts := parseIntList(*numPerturb)
		for _, nr := range randomCounts {
			for _, np := range perturbCounts {
				params := solver.Params{
					NumRandom:  nr,
					NumPerturb: np,
					PerturbMin: *perturbMin,
					PerturbMax: *perturbMax,
				}
				var results []runResult
				for run := range *runs {
					rng := rand.New(rand.NewSource(int64(run * 31337)))
					start := time.Now()
					sols := solver.Solve(n, trip.RoomSize, trip.PreferNotMultiple, trip.NoPreferCost, constraints, params, rng)
					elapsed := time.Since(start)
					if len(sols) > 0 {
						var assignments [][]int
						for _, s := range sols {
							assignments = append(assignments, s.Assignment)
						}
						results = append(results, runResult{sols[0].Score, assignments, elapsed})
					}
				}
				label := fmt.Sprintf("hillclimb random=%d perturb=%d pmin=%d pmax=%d", nr, np, *perturbMin, *perturbMax)
				printStats(label, results, *runs)
			}
		}
	}

	if *algo == "fast" || *algo == "both" || *algo == "all" {
		randomCounts := parseIntList(*numRandom)
		perturbCounts := parseIntList(*numPerturb)
		for _, nr := range randomCounts {
			for _, np := range perturbCounts {
				params := solver.Params{
					NumRandom:  nr,
					NumPerturb: np,
					PerturbMin: *perturbMin,
					PerturbMax: *perturbMax,
				}
				var results []runResult
				for run := range *runs {
					rng := rand.New(rand.NewSource(int64(run * 31337)))
					start := time.Now()
					sols := solver.SolveFast(n, trip.RoomSize, trip.PreferNotMultiple, trip.NoPreferCost, constraints, params, rng)
					elapsed := time.Since(start)
					if len(sols) > 0 {
						var assignments [][]int
						for _, s := range sols {
							assignments = append(assignments, s.Assignment)
						}
						results = append(results, runResult{sols[0].Score, assignments, elapsed})
					}
				}
				label := fmt.Sprintf("fast random=%d perturb=%d pmin=%d pmax=%d", nr, np, *perturbMin, *perturbMax)
				printStats(label, results, *runs)
			}
		}
	}

	if *algo == "sa" || *algo == "all" {
		restartCounts := parseIntList(*saRestarts)
		stepCounts := parseIntList(*saSteps)
		for _, nr := range restartCounts {
			for _, ns := range stepCounts {
				params := solver.SAParams{
					Restarts: nr,
					Steps:    ns,
					TempHigh: *saTempHigh,
					TempLow:  *saTempLow,
				}
				var results []runResult
				for run := range *runs {
					rng := rand.New(rand.NewSource(int64(run * 31337)))
					start := time.Now()
					sols := solver.SolveSA(n, trip.RoomSize, trip.PreferNotMultiple, trip.NoPreferCost, constraints, params, rng)
					elapsed := time.Since(start)
					if len(sols) > 0 {
						var assignments [][]int
						for _, s := range sols {
							assignments = append(assignments, s.Assignment)
						}
						results = append(results, runResult{sols[0].Score, assignments, elapsed})
					}
				}
				label := fmt.Sprintf("sa restarts=%d steps=%d thigh=%.1f tlow=%.3f", nr, ns, *saTempHigh, *saTempLow)
				printStats(label, results, *runs)
			}
		}
	}

	if *algo == "hybrid" || *algo == "both" || *algo == "all" {
		restartCounts := parseIntList(*saRestarts)
		stepCounts := parseIntList(*hybridSteps)
		for _, nr := range restartCounts {
			for _, ns := range stepCounts {
				params := solver.HybridParams{
					SARestarts: nr,
					SASteps:    ns,
					TempHigh:   *hybridTempHigh,
					TempLow:    *hybridTempLow,
				}
				var results []runResult
				for run := range *runs {
					rng := rand.New(rand.NewSource(int64(run * 31337)))
					start := time.Now()
					sols := solver.SolveHybrid(n, trip.RoomSize, trip.PreferNotMultiple, trip.NoPreferCost, constraints, params, rng)
					elapsed := time.Since(start)
					if len(sols) > 0 {
						var assignments [][]int
						for _, s := range sols {
							assignments = append(assignments, s.Assignment)
						}
						results = append(results, runResult{sols[0].Score, assignments, elapsed})
					}
				}
				label := fmt.Sprintf("hybrid restarts=%d steps=%d thigh=%.1f tlow=%.3f", nr, ns, *hybridTempHigh, *hybridTempLow)
				printStats(label, results, *runs)
			}
		}
	}
}

func parseIntList(s string) []int {
	parts := strings.Split(s, ",")
	var result []int
	for _, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err == nil {
			result = append(result, v)
		}
	}
	return result
}
