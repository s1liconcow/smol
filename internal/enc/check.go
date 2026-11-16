package enc

import (
	"fmt"

	"strings"
)

type MutationCounts struct{ Total, Accepted, Rejected, Header, HeaderAccepted, Body, BodyAccepted int }
type CheckResult struct {
	RoundTrips int
	Mutations  MutationCounts
	PatchBytes int
}

func Check(base, target Tree, mutate bool) (CheckResult, error) {
	return CheckWithOptions(base, target, mutate, Options{})
}

func CheckWithOptions(base, target Tree, mutate bool, options Options) (CheckResult, error) {
	if options.analysis == nil {
		options.analysis = newAnalysis()
	}
	var result CheckResult
	normalized := Tree{}
	for p, t := range target {
		b, e := Normalize(t)
		if e != nil {
			return result, e
		}
		normalized[p] = b
	}
	for _, profile := range []Profile{E0, E1, E2} {
		patch, e := Encode(base, target, withProfile(options, profile))
		if e != nil {
			return result, e
		}
		r, e := Decode(base, patch, withProfile(options, profile))
		if e != nil || !equalTrees(r.Files, normalized) {
			return result, fmt.Errorf("%s round trip: %v; %s", profile, e, mismatch(r, normalized))
		}
		result.RoundTrips++
		if profile == E0 {
			for _, decoder := range []Profile{E1, E2} {
				r, e = Decode(base, patch, withProfile(options, decoder))
				if e != nil || !equalTrees(r.Files, normalized) {
					return result, fmt.Errorf("plain Go decoded with %s: %v; %s", decoder, e, mismatch(r, normalized))
				}
				result.RoundTrips++
			}
		}
		if profile != E1 || !mutate || patch == "" {
			continue
		}
		result.PatchBytes = len(patch)
		ls := strings.Split(strings.TrimSuffix(patch, "\n"), "\n")
		for i, line := range ls {
			for _, mode := range []int{0, 1, 2} {
				copy := append([]string(nil), ls...)
				switch mode {
				case 0:
					copy = append(copy[:i], copy[i+1:]...)
				case 1:
					copy = append(copy[:i], append([]string{line}, copy[i:]...)...)
				case 2:
					copy[i] = string([]rune(line)[:len([]rune(line))/2])
				}
				result.Mutations.Total++
				header := marker(line) != ""
				if header {
					result.Mutations.Header++
				} else {
					result.Mutations.Body++
				}
				r, e := Decode(base, strings.Join(copy, "\n")+"\n", withProfile(options, E1))
				if e != nil {
					result.Mutations.Rejected++
					continue
				}
				for p, text := range r.Files {
					if text == base[p] {
						continue
					}
					if _, _, e := parseGo(text); e != nil {
						return result, fmt.Errorf("accepted mutation does not parse: %w", e)
					}
				}
				result.Mutations.Accepted++
				if header {
					result.Mutations.HeaderAccepted++
				} else {
					result.Mutations.BodyAccepted++
				}
			}
		}
	}
	return result, nil
}

func withProfile(o Options, p Profile) Options { o.Profile = p; return o }

func mismatch(r *Result, want Tree) string {
	if r == nil {
		return "no result"
	}
	for p, text := range want {
		got, ok := r.Files[p]
		if !ok {
			return "missing " + p
		}
		if got == text {
			continue
		}
		a, b := lines(text), lines(got)
		for i := 0; i < min(len(a), len(b)); i++ {
			if a[i] != b[i] {
				return fmt.Sprintf("%s:%d: want %q; got %q", p, i+1, a[i], b[i])
			}
		}
		return fmt.Sprintf("%s: line count %d vs %d", p, len(a), len(b))
	}
	return "extra file"
}
