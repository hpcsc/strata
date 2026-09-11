package diff

type Status string

const (
	Added       Status = "A"
	Modified    Status = "M"
	Deleted     Status = "D"
	Renamed     Status = "R"
	Copied      Status = "C"
	TypeChanged Status = "T"
)

type File struct {
	Path string
	// OldPath is set when the file was renamed or copied.
	OldPath string
	Status  Status
	// OldBlob and NewBlob are git object IDs; a side the file does not exist
	// on has no blob.
	OldBlob    string
	NewBlob    string
	Insertions int
	Deletions  int
}

func (f File) Paths() []string {
	if f.OldPath != "" && f.OldPath != f.Path {
		return []string{f.OldPath, f.Path}
	}
	return []string{f.Path}
}
