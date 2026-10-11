// semver.go — v1.2.9 semantic-version comparison: the single version
// grammar for app updates (replaces the ad-hoc dotted-numeric
// comparator that disagreed with semver on prerelease ordering).
// Split out of appupdate.go (behavior-identical file-level move).
package updater

import (
	"strconv"
	"strings"
)

// CompareVersions orders two version strings by SEMANTIC VERSION rules
// (the single version grammar for app updates — v1.2.9 replaced the
// ad-hoc dotted-numeric+lexical comparator, which disagreed with
// semver on prerelease ordering and malformed inputs):
//
//   - numeric core compared numerically, component by component
//     (1.2.10 > 1.2.9);
//   - a prerelease (1.3.0-beta.1) sorts BELOW its release (1.3.0) —
//     semver §11;
//   - prerelease identifiers compare per semver: numeric identifiers
//     numerically, alphanumeric lexically; numeric sorts below
//     alphanumeric; a longer prerelease list with an identical prefix
//     sorts HIGHER;
//   - shorter vs longer core (1.2 vs 1.2.0) compares missing
//     components as 0 (equal);
//   - build metadata (+…) is ignored, per semver §10;
//   - malformed input compares LOW (a garbage version is never equal to
//     or newer than a valid one) — the caller's "not up to date" path is
//     the safe outcome for unparseable strings.
func CompareVersions(a, b string) int {
	ac, ap := splitSemver(a)
	bc, bp := splitSemver(b)

	n := len(ac)
	if len(bc) > n {
		n = len(bc)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(ac) {
			av = ac[i]
		}
		if i < len(bc) {
			bv = bc[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}

	// Equal cores: prerelease presence decides (release > prerelease).
	switch {
	case ap == "" && bp == "":
		return 0
	case ap == "":
		return 1 // a is the release, b is a prerelease
	case bp == "":
		return -1
	}
	return comparePrerelease(ap, bp)
}

// splitSemver splits v into numeric core components and the prerelease
// string ("" when none). Malformed input yields a zero core, which
// compares LOW (never reports garbage as newer). Build metadata (+…) is
// stripped first, per semver \u00a710.
func splitSemver(v string) ([]int, string) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i] // build metadata ignored
	}
	core, pre := v, ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		core, pre = v[:i], v[i+1:]
	}

	parts := strings.Split(core, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			out = append(out, 0)
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			out = append(out, 0)
			continue
		}
		out = append(out, n)
	}
	return out, pre
}

// comparePrerelease orders two prerelease strings per semver \u00a711:
// identifier-by-identifier over the SHARED length (numeric identifiers
// numerically and below alphanumeric, otherwise lexical), then — with
// all shared identifiers equal — the LONGER list has HIGHER precedence.
func comparePrerelease(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	shared := len(as)
	if len(bs) < shared {
		shared = len(bs)
	}
	for i := 0; i < shared; i++ {
		ai, bi := as[i], bs[i]
		if ai == bi {
			continue
		}
		an, aerr := strconv.Atoi(ai)
		bn, berr := strconv.Atoi(bi)
		switch {
		case aerr == nil && berr == nil:
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
		case aerr == nil:
			return -1 // numeric identifiers sort below alphanumeric
		case berr == nil:
			return 1
		default:
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	if len(as) != len(bs) {
		if len(as) < len(bs) {
			return -1 // smaller set = lower precedence (semver \u00a711)
		}
		return 1
	}
	return 0
}
