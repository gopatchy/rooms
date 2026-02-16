package solver

import (
	"math/rand"
	"slices"
	"strconv"
	"strings"
)

type Constraint struct {
	StudentA int
	StudentB int
	Kind     string
}

type Params struct {
	NumRandom  int
	NumPerturb int
	PerturbMin int
	PerturbMax int
}

var DefaultParams = Params{
	NumRandom:  50,
	NumPerturb: 750,
	PerturbMin: 3,
	PerturbMax: 8,
}

type Solution struct {
	Assignment []int
	Score      int
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

type solverState struct {
	n         int
	roomSizes []int
	numRooms  int
	pnMultiple int
	npCost     int

	constraints []Constraint
	mustApart   map[[2]int]bool

	groups      map[int][]int
	groupList   [][]int
	groupOf     []int
	uniqueGroups []int

	studentConstraints [][]int
	hasPrefer          []bool
	preferFrom         [][]int
	mustApartFor       [][]int
}

func newSolverState(n int, roomSizes []int, pnMultiple, npCost int, constraints []Constraint) *solverState {
	s := &solverState{
		n:          n,
		roomSizes:  roomSizes,
		numRooms:   len(roomSizes),
		pnMultiple: pnMultiple,
		npCost:     npCost,
		constraints: constraints,
		mustApart:  map[[2]int]bool{},
	}

	mustTogether := map[[2]int]bool{}
	for _, c := range constraints {
		switch c.Kind {
		case "must":
			p := [2]int{c.StudentA, c.StudentB}
			if p[0] > p[1] {
				p[0], p[1] = p[1], p[0]
			}
			mustTogether[p] = true
		case "must_not":
			p := [2]int{c.StudentA, c.StudentB}
			if p[0] > p[1] {
				p[0], p[1] = p[1], p[0]
			}
			s.mustApart[p] = true
		}
	}

	uf := make([]int, n)
	for i := range uf {
		uf[i] = i
	}
	var ufFind func(int) int
	ufFind = func(x int) int {
		if uf[x] != x {
			uf[x] = ufFind(uf[x])
		}
		return uf[x]
	}
	for p := range mustTogether {
		ra, rb := ufFind(p[0]), ufFind(p[1])
		if ra != rb {
			uf[ra] = rb
		}
	}

	s.groups = map[int][]int{}
	for i := range n {
		root := ufFind(i)
		s.groups[root] = append(s.groups[root], i)
	}

	s.groupOf = make([]int, n)
	for root, members := range s.groups {
		for _, m := range members {
			s.groupOf[m] = root
		}
	}

	s.groupList = make([][]int, 0, len(s.groups))
	for _, members := range s.groups {
		s.groupList = append(s.groupList, members)
	}
	slices.SortFunc(s.groupList, func(a, b []int) int { return len(b) - len(a) })

	s.uniqueGroups = make([]int, 0, len(s.groups))
	for root := range s.groups {
		s.uniqueGroups = append(s.uniqueGroups, root)
	}
	slices.Sort(s.uniqueGroups)

	s.studentConstraints = make([][]int, n)
	s.hasPrefer = make([]bool, n)
	s.preferFrom = make([][]int, n)
	s.mustApartFor = make([][]int, n)
	for ci, c := range constraints {
		s.studentConstraints[c.StudentA] = append(s.studentConstraints[c.StudentA], ci)
		s.studentConstraints[c.StudentB] = append(s.studentConstraints[c.StudentB], ci)
		if c.Kind == "prefer" {
			s.hasPrefer[c.StudentA] = true
			s.preferFrom[c.StudentA] = append(s.preferFrom[c.StudentA], ci)
		}
	}
	for p := range s.mustApart {
		s.mustApartFor[p[0]] = append(s.mustApartFor[p[0]], p[1])
		s.mustApartFor[p[1]] = append(s.mustApartFor[p[1]], p[0])
	}

	return s
}

func (s *solverState) hasHardConflict() bool {
	uf := make([]int, s.n)
	for i := range uf {
		uf[i] = i
	}
	var ufFind func(int) int
	ufFind = func(x int) int {
		if uf[x] != x {
			uf[x] = ufFind(uf[x])
		}
		return uf[x]
	}
	for _, c := range s.constraints {
		if c.Kind == "must" {
			ra, rb := ufFind(c.StudentA), ufFind(c.StudentB)
			if ra != rb {
				uf[ra] = rb
			}
		}
	}
	for p := range s.mustApart {
		if ufFind(p[0]) == ufFind(p[1]) {
			return true
		}
	}
	return false
}

func (s *solverState) score(assignment []int) int {
	sc := 0
	gotPrefer := make([]bool, s.n)
	for _, c := range s.constraints {
		sameRoom := assignment[c.StudentA] == assignment[c.StudentB]
		switch c.Kind {
		case "prefer":
			if sameRoom {
				sc++
				gotPrefer[c.StudentA] = true
			}
		case "prefer_not":
			if sameRoom {
				sc -= s.pnMultiple
			}
		}
	}
	for i := range s.n {
		if s.hasPrefer[i] && !gotPrefer[i] {
			sc -= s.npCost
		}
	}
	return sc
}

func (s *solverState) feasibleForGroup(assignment []int, groupRoot int, room int) bool {
	for _, m := range s.groups[groupRoot] {
		for _, partner := range s.mustApartFor[m] {
			if s.groupOf[partner] != groupRoot && assignment[partner] == room {
				return false
			}
		}
	}
	return true
}

func (s *solverState) fastHillClimb(assignment []int) int {
	n := s.n
	numRooms := s.numRooms

	roomCounts := make([]int, numRooms)
	for _, room := range assignment {
		roomCounts[room]++
	}

	prefSatCount := make([]int, n)
	for _, c := range s.constraints {
		if c.Kind == "prefer" && assignment[c.StudentA] == assignment[c.StudentB] {
			prefSatCount[c.StudentA]++
		}
	}

	currentScore := 0
	for _, c := range s.constraints {
		if assignment[c.StudentA] == assignment[c.StudentB] {
			switch c.Kind {
			case "prefer":
				currentScore++
			case "prefer_not":
				currentScore -= s.pnMultiple
			}
		}
	}
	for i := range n {
		if s.hasPrefer[i] && prefSatCount[i] == 0 {
			currentScore -= s.npCost
		}
	}

	memberSet := make([]bool, n)

	deltaForMove := func(groupRoot int, oldRoom, newRoom int) int {
		members := s.groups[groupRoot]
		for _, m := range members {
			memberSet[m] = true
		}

		delta := 0
		npAffected := make(map[int]int)

		for _, m := range members {
			for _, ci := range s.studentConstraints[m] {
				c := s.constraints[ci]
				other := c.StudentB
				if other == m {
					other = c.StudentA
				}
				if memberSet[other] {
					continue
				}
				otherRoom := assignment[other]
				wasSame := otherRoom == oldRoom
				willBeSame := otherRoom == newRoom
				if wasSame == willBeSame {
					continue
				}
				switch c.Kind {
				case "prefer":
					if wasSame {
						delta--
						npAffected[c.StudentA]--
					} else {
						delta++
						npAffected[c.StudentA]++
					}
				case "prefer_not":
					if wasSame {
						delta += s.pnMultiple
					} else {
						delta -= s.pnMultiple
					}
				}
			}
		}

		for student, change := range npAffected {
			if !s.hasPrefer[student] {
				continue
			}
			wasSat := prefSatCount[student] > 0
			willBeSat := prefSatCount[student]+change > 0
			if wasSat && !willBeSat {
				delta -= s.npCost
			} else if !wasSat && willBeSat {
				delta += s.npCost
			}
		}

		for _, m := range members {
			memberSet[m] = false
		}
		return delta
	}

	applyMove := func(groupRoot int, oldRoom, newRoom int) {
		members := s.groups[groupRoot]
		for _, m := range members {
			memberSet[m] = true
		}
		for _, m := range members {
			for _, ci := range s.studentConstraints[m] {
				c := s.constraints[ci]
				other := c.StudentB
				if other == m {
					other = c.StudentA
				}
				if memberSet[other] {
					continue
				}
				otherRoom := assignment[other]
				wasSame := otherRoom == oldRoom
				willBeSame := otherRoom == newRoom
				if wasSame == willBeSame {
					continue
				}
				if c.Kind == "prefer" {
					if wasSame {
						prefSatCount[c.StudentA]--
					} else {
						prefSatCount[c.StudentA]++
					}
				}
			}
		}
		roomCounts[oldRoom] -= len(members)
		for _, m := range members {
			assignment[m] = newRoom
		}
		roomCounts[newRoom] += len(members)
		for _, m := range members {
			memberSet[m] = false
		}
	}

	for {
		bestDelta := 0
		bestGi := -1
		bestTarget := -1
		bestSwapGj := -1

		for gi, gRoot := range s.uniqueGroups {
			grp := s.groups[gRoot]
			gRoom := assignment[grp[0]]

			for room := range numRooms {
				if room == gRoom {
					continue
				}
				if roomCounts[room]+len(grp) > s.roomSizes[room] {
					continue
				}
				if !s.feasibleForGroup(assignment, gRoot, room) {
					continue
				}
				delta := deltaForMove(gRoot, gRoom, room)
				if delta > bestDelta {
					bestDelta = delta
					bestGi = gi
					bestTarget = room
					bestSwapGj = -1
				}
			}

			for gj := gi + 1; gj < len(s.uniqueGroups); gj++ {
				g2Root := s.uniqueGroups[gj]
				grp2 := s.groups[g2Root]
				g2Room := assignment[grp2[0]]
				if gRoom == g2Room {
					continue
				}
				newGRoom := roomCounts[gRoom] - len(grp) + len(grp2)
				newG2Room := roomCounts[g2Room] - len(grp2) + len(grp)
				if newGRoom > s.roomSizes[gRoom] || newG2Room > s.roomSizes[g2Room] {
					continue
				}
				if !s.feasibleForGroup(assignment, gRoot, g2Room) {
					continue
				}
				delta1 := deltaForMove(gRoot, gRoom, g2Room)
				applyMove(gRoot, gRoom, g2Room)
				if !s.feasibleForGroup(assignment, g2Root, gRoom) {
					applyMove(gRoot, g2Room, gRoom)
					continue
				}
				delta2 := deltaForMove(g2Root, g2Room, gRoom)
				applyMove(gRoot, g2Room, gRoom)

				totalDelta := delta1 + delta2
				if totalDelta > bestDelta {
					bestDelta = totalDelta
					bestGi = gi
					bestTarget = -1
					bestSwapGj = gj
				}
			}
		}

		if bestDelta <= 0 {
			break
		}

		gRoot := s.uniqueGroups[bestGi]
		gRoom := assignment[s.groups[gRoot][0]]
		if bestSwapGj < 0 {
			applyMove(gRoot, gRoom, bestTarget)
		} else {
			g2Root := s.uniqueGroups[bestSwapGj]
			g2Room := assignment[s.groups[g2Root][0]]
			applyMove(gRoot, gRoom, g2Room)
			applyMove(g2Root, g2Room, gRoom)
		}
		currentScore += bestDelta
	}
	return currentScore
}

func (s *solverState) initialPlacement(assignment []int) bool {
	roomCap := make([]int, s.numRooms)
	copy(roomCap, s.roomSizes)

	var placeGroups func(gi int) bool
	placeGroups = func(gi int) bool {
		if gi >= len(s.groupList) {
			return true
		}
		grp := s.groupList[gi]
		for room := range s.numRooms {
			if roomCap[room] < len(grp) {
				continue
			}
			ok := true
			for _, member := range grp {
				for p := range s.mustApart {
					partner := -1
					if p[0] == member {
						partner = p[1]
					}
					if p[1] == member {
						partner = p[0]
					}
					if partner >= 0 && assignment[partner] == room {
						alreadyPlaced := false
						for gj := range gi {
							if slices.Contains(s.groupList[gj], partner) {
								alreadyPlaced = true
								break
							}
						}
						if alreadyPlaced {
							ok = false
							break
						}
					}
				}
				if !ok {
					break
				}
			}
			if !ok {
				continue
			}
			for _, member := range grp {
				assignment[member] = room
			}
			roomCap[room] -= len(grp)
			if placeGroups(gi + 1) {
				return true
			}
			roomCap[room] += len(grp)
		}
		return false
	}
	return placeGroups(0)
}

func (s *solverState) randomPlacement(assignment []int, rng *rand.Rand) bool {
	roomCap := make([]int, s.numRooms)
	copy(roomCap, s.roomSizes)
	perm := rng.Perm(len(s.groupList))
	for _, pi := range perm {
		grp := s.groupList[pi]
		placed := false
		order := rng.Perm(s.numRooms)
		for _, room := range order {
			if roomCap[room] < len(grp) {
				continue
			}
			valid := true
			for _, member := range grp {
				for p := range s.mustApart {
					partner := -1
					if p[0] == member {
						partner = p[1]
					}
					if p[1] == member {
						partner = p[0]
					}
					if partner >= 0 && assignment[partner] == room {
						valid = false
						break
					}
				}
				if !valid {
					break
				}
			}
			if !valid {
				continue
			}
			for _, member := range grp {
				assignment[member] = room
			}
			roomCap[room] -= len(grp)
			placed = true
			break
		}
		if !placed {
			return false
		}
	}
	return true
}

type solutionTracker struct {
	bestScore     int
	bestSolutions [][]int
	seen          map[string]bool
}

func newTracker(initial []int, score int) *solutionTracker {
	t := &solutionTracker{
		bestScore: score,
		seen:      map[string]bool{},
	}
	key := normalizeKey(initial)
	t.seen[key] = true
	t.bestSolutions = append(t.bestSolutions, slices.Clone(initial))
	return t
}

func (t *solutionTracker) add(a []int, s int) {
	if s > t.bestScore {
		t.bestScore = s
		t.bestSolutions = nil
		t.seen = map[string]bool{}
	}
	if s == t.bestScore {
		key := normalizeKey(a)
		if !t.seen[key] {
			t.seen[key] = true
			t.bestSolutions = append(t.bestSolutions, slices.Clone(a))
		}
	}
}

func SolveFast(n int, roomSizes []int, pnMultiple, npCost int, constraints []Constraint, params Params, rng *rand.Rand) []Solution {
	if n == 0 {
		return nil
	}

	st := newSolverState(n, roomSizes, pnMultiple, npCost, constraints)
	if st.hasHardConflict() {
		return nil
	}

	assignment := make([]int, n)
	if !st.initialPlacement(assignment) {
		for i := range n {
			assignment[i] = i % st.numRooms
		}
	}

	initialAssignment := slices.Clone(assignment)
	tracker := newTracker(assignment, st.score(assignment))

	roomCount := func(a []int, room int) int {
		c := 0
		for _, r := range a {
			if r == room {
				c++
			}
		}
		return c
	}

	feasible := func(a []int) bool {
		for p := range st.mustApart {
			if a[p[0]] == a[p[1]] {
				return false
			}
		}
		rc := map[int]int{}
		for _, room := range a {
			rc[room]++
		}
		for room, cnt := range rc {
			if cnt > st.roomSizes[room] {
				return false
			}
		}
		return true
	}

	perturb := func(src []int, count int) {
		copy(assignment, src)
		indices := rng.Perm(len(st.uniqueGroups))
		count = min(count, len(indices))
		for _, gi := range indices[:count] {
			grp := st.groups[st.uniqueGroups[gi]]
			oldRoom := assignment[grp[0]]
			rooms := rng.Perm(st.numRooms)
			for _, room := range rooms {
				if room == oldRoom {
					continue
				}
				if roomCount(assignment, room)+len(grp) > st.roomSizes[room] {
					continue
				}
				for _, m := range grp {
					assignment[m] = room
				}
				if feasible(assignment) {
					break
				}
				for _, m := range grp {
					assignment[m] = oldRoom
				}
			}
		}
	}

	copy(assignment, initialAssignment)
	tracker.add(assignment, st.fastHillClimb(assignment))

	for range params.NumRandom {
		if st.randomPlacement(assignment, rng) {
			tracker.add(assignment, st.fastHillClimb(assignment))
		}
	}

	for range params.NumPerturb {
		src := tracker.bestSolutions[rng.Intn(len(tracker.bestSolutions))]
		perturb(src, params.PerturbMin+rng.Intn(params.PerturbMax-params.PerturbMin))
		tracker.add(assignment, st.fastHillClimb(assignment))
	}

	results := make([]Solution, len(tracker.bestSolutions))
	for i, sol := range tracker.bestSolutions {
		results[i] = Solution{Assignment: sol, Score: tracker.bestScore}
	}
	return results
}

