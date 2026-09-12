package git

import (
	"fmt"
	"strconv"
	"strings"
)

type Version struct {
	Major int
	Minor int
	Patch int
}

func ParseVersion(out string) (Version, error) {
	text := strings.TrimSpace(out)
	fields := strings.Fields(text)
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return Version{}, fmt.Errorf("%q is not a git version", text)
	}
	parts := strings.Split(fields[2], ".")
	var numbers []int
	for _, part := range parts[:min(3, len(parts))] {
		n, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		numbers = append(numbers, n)
	}
	if len(numbers) < 2 {
		return Version{}, fmt.Errorf("%q is not a git version", text)
	}
	v := Version{Major: numbers[0], Minor: numbers[1]}
	if len(numbers) == 3 {
		v.Patch = numbers[2]
	}
	return v, nil
}

func (v Version) AtLeast(other Version) bool {
	if v.Major != other.Major {
		return v.Major > other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor > other.Minor
	}
	return v.Patch >= other.Patch
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}
