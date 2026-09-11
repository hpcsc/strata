package diff

type LineKind int

const (
	Context LineKind = iota
	Deletion
	Addition
)

type Line struct {
	Kind LineKind
	Text string
	// OldNumber is 0 for an added line; NewNumber is 0 for a deleted line.
	OldNumber int
	NewNumber int
}

type Hunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	// Section is the text git prints after the second @@, usually the
	// enclosing function.
	Section string
	Lines   []Line
}

type Patch struct {
	File    File
	Binary  bool
	OldMode string
	NewMode string
	Hunks   []Hunk
}

// Summary describes a patch that has no lines to show, and is empty otherwise.
func (p Patch) Summary() string {
	switch {
	case p.Binary:
		return "binary file changed"
	case len(p.Hunks) > 0:
		return ""
	case p.File.OldPath != "" && p.File.OldPath != p.File.Path:
		return "renamed from " + p.File.OldPath + " with no other changes"
	case p.OldMode != "" && p.NewMode != "" && p.OldMode != p.NewMode:
		return "mode changed from " + p.OldMode + " to " + p.NewMode
	default:
		return "no lines changed"
	}
}
