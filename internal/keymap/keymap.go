package keymap

import (
	"errors"
	"fmt"
	"slices"
)

type Table int

const (
	Anywhere Table = iota
	InStack
	InFiles
	InDiff
	InSearch
	InPlan
	tableCount
)

var tableNames = [tableCount]string{"keys", "keys.stack", "keys.files", "keys.diff", "keys.search", "keys.plan"}

func Tables() []Table {
	return []Table{Anywhere, InStack, InFiles, InDiff, InSearch, InPlan}
}

func TableNamed(name string) (Table, bool) {
	i := slices.Index(tableNames[:], name)
	return Table(i), i >= 0
}

func (t Table) String() string {
	return tableNames[t]
}

func (t Table) Actions() []Action {
	var actions []Action
	for a := range actionCount {
		if a != None && definitions[a].table == t {
			actions = append(actions, a)
		}
	}
	return actions
}

func (t Table) panel() bool {
	return t == InStack || t == InFiles || t == InDiff
}

type Action int

const (
	None Action = iota
	Quit
	Help
	StartSearch
	NextPanel
	PreviousPanel
	NextBranch
	PreviousBranch
	ToggleSplit
	ToggleZoom
	ToggleTree
	ToggleViewed
	Refresh
	Sync
	StackDown
	StackUp
	StackTop
	StackBottom
	StackOpen
	FilesDown
	FilesUp
	FilesTop
	FilesBottom
	FilesFold
	FilesOpen
	FilesBack
	FilesHalfPageDown
	FilesHalfPageUp
	DiffDown
	DiffUp
	DiffHalfPageDown
	DiffHalfPageUp
	DiffPageDown
	DiffPageUp
	DiffTop
	DiffBottom
	NextHunk
	PreviousHunk
	NextFile
	PreviousFile
	DiffBack
	ClearSearch
	NextMatch
	PreviousMatch
	MoveStack
	ResolveConflict
	ClosePlan
	actionCount
)

type definition struct {
	table Table
	name  string
	keys  []string
	about string
}

var definitions = [actionCount]definition{
	Quit:              {Anywhere, "quit", []string{"Q"}, "quit"},
	Help:              {Anywhere, "help", []string{"?"}, "show all keys"},
	StartSearch:       {Anywhere, "start_search", []string{"/"}, "filter the panel, or find text in the diff"},
	NextPanel:         {Anywhere, "next_panel", []string{"tab"}, "next panel"},
	PreviousPanel:     {Anywhere, "previous_panel", []string{"shift+tab"}, "previous panel"},
	NextBranch:        {Anywhere, "next_branch", []string{"]"}, "next branch; the same file stays selected"},
	PreviousBranch:    {Anywhere, "previous_branch", []string{"["}, "previous branch; the same file stays selected"},
	ToggleSplit:       {Anywhere, "toggle_split", []string{"s"}, "side by side or unified diff"},
	ToggleZoom:        {Anywhere, "toggle_zoom", []string{"z"}, "diff on the full screen"},
	ToggleTree:        {Anywhere, "toggle_tree", []string{"t"}, "files as a tree or as a list of paths"},
	ToggleViewed:      {Anywhere, "toggle_viewed", []string{"v"}, "mark the file viewed, then go to the next file"},
	Refresh:           {Anywhere, "refresh", []string{"r"}, "read the branches again"},
	Sync:              {Anywhere, "sync", []string{"S"}, "fetch the trunk and show the plan of a sync"},
	StackDown:         {InStack, "down", []string{"j", "down"}, "next branch"},
	StackUp:           {InStack, "up", []string{"k", "up"}, "previous branch"},
	StackTop:          {InStack, "top", []string{"g", "home"}, "first branch"},
	StackBottom:       {InStack, "bottom", []string{"G", "end"}, "last branch"},
	StackOpen:         {InStack, "open", []string{"enter", "l", "right"}, "go to the files of the branch"},
	FilesDown:         {InFiles, "down", []string{"j", "down"}, "next file or folder"},
	FilesUp:           {InFiles, "up", []string{"k", "up"}, "previous file or folder"},
	FilesTop:          {InFiles, "top", []string{"g", "home"}, "first row"},
	FilesBottom:       {InFiles, "bottom", []string{"G", "end"}, "last row"},
	FilesFold:         {InFiles, "fold", []string{"o"}, "fold or unfold the folder"},
	FilesOpen:         {InFiles, "open", []string{"enter", "l", "right"}, "go to the diff"},
	FilesBack:         {InFiles, "back", []string{"q", "h", "left", "esc"}, "back to the stack"},
	FilesHalfPageDown: {InFiles, "half_page_down", []string{"ctrl+d"}, "scroll the diff down half a page"},
	FilesHalfPageUp:   {InFiles, "half_page_up", []string{"ctrl+u"}, "scroll the diff up half a page"},
	DiffDown:          {InDiff, "down", []string{"j", "down"}, "scroll down one line"},
	DiffUp:            {InDiff, "up", []string{"k", "up"}, "scroll up one line"},
	DiffHalfPageDown:  {InDiff, "half_page_down", []string{"ctrl+d"}, "scroll down half a page"},
	DiffHalfPageUp:    {InDiff, "half_page_up", []string{"ctrl+u"}, "scroll up half a page"},
	DiffPageDown:      {InDiff, "page_down", []string{"space", "pgdown", "ctrl+f"}, "scroll down a full page"},
	DiffPageUp:        {InDiff, "page_up", []string{"b", "pgup", "ctrl+b"}, "scroll up a full page"},
	DiffTop:           {InDiff, "top", []string{"g", "home"}, "go to the top"},
	DiffBottom:        {InDiff, "bottom", []string{"G", "end"}, "go to the bottom"},
	NextHunk:          {InDiff, "next_hunk", []string{"n"}, "next hunk"},
	PreviousHunk:      {InDiff, "previous_hunk", []string{"p"}, "previous hunk"},
	NextFile:          {InDiff, "next_file", []string{"J"}, "next file, past folded folders"},
	PreviousFile:      {InDiff, "previous_file", []string{"K"}, "previous file, past folded folders"},
	DiffBack:          {InDiff, "back", []string{"q", "h", "left", "esc"}, "back to the files; in zoom, end the zoom"},
	ClearSearch:       {InSearch, "clear", []string{"esc"}, "clear the search"},
	NextMatch:         {InSearch, "next_match", []string{"n"}, "next match in the diff"},
	PreviousMatch:     {InSearch, "previous_match", []string{"N"}, "previous match in the diff"},
	MoveStack:         {InPlan, "move_stack", []string{"enter"}, "move the stack of the selected branch"},
	ResolveConflict:   {InPlan, "resolve_conflict", []string{"c"}, "resolve the conflict of the stack in a shell"},
	ClosePlan:         {InPlan, "close", []string{"q", "esc"}, "close the plan"},
}

func Named(t Table, name string) (Action, bool) {
	for _, a := range t.Actions() {
		if definitions[a].name == name {
			return a, true
		}
	}
	return None, false
}

func (a Action) Table() Table {
	return definitions[a].table
}

func (a Action) Name() string {
	return definitions[a].name
}

func (a Action) About() string {
	return definitions[a].about
}

func (a Action) String() string {
	return definitions[a].table.String() + "." + definitions[a].name
}

type Keymap struct {
	keys [actionCount][]string
}

func Default() Keymap {
	var k Keymap
	for a, d := range definitions {
		k.keys[a] = d.keys
	}
	return k
}

func (k Keymap) Keys(a Action) []string {
	return k.keys[a]
}

func (k Keymap) Action(t Table, key string) Action {
	for _, a := range t.Actions() {
		if slices.Contains(k.keys[a], key) {
			return a
		}
	}
	return None
}

func (k *Keymap) Set(a Action, keys []string) error {
	var names []string
	for _, key := range keys {
		name, err := canonical(key)
		if err != nil {
			return err
		}
		if name == "ctrl+c" {
			return errors.New("ctrl+c always quits, so no action can have it")
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	k.keys[a] = names
	return nil
}

func (k Keymap) Check() error {
	var problems []error
	for a := range actionCount {
		for b := a + 1; b < actionCount; b++ {
			if !clash(a.Table(), b.Table()) {
				continue
			}
			for _, key := range k.keys[a] {
				if slices.Contains(k.keys[b], key) {
					problems = append(problems, fmt.Errorf("%q is a key of %s and of %s", key, a, b))
				}
			}
		}
	}
	return errors.Join(problems...)
}

// the ui looks in Anywhere before a panel, so a panel action with a key of
// Anywhere never runs.
func clash(s, t Table) bool {
	return s == t || (s == Anywhere && t.panel()) || (s.panel() && t == Anywhere)
}
