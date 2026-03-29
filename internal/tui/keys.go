package tui

type pane int

const (
	projectPane pane = iota
	itemPane
)

type appMode int

const (
	modeNormal        appMode = iota
	modeAddItem               // text input: new item in current project
	modeAddItemMulti          // text input → project picker: multi-project add
	modeAddProject            // text input: new project
	modeEditTitle             // text input: edit existing item title
	modeProjectPicker         // checkbox picker: manage project membership
	modeItemDetail            // expanded item view (read-only with action keys)
	modeEditNotes             // multiline textarea for notes editing
	modeHelp                  // keybinding reference overlay
	modeSearch                // search text input + results
	modeMove                  // grab item, j/k to reposition
	modeDepLink               // pick item to add as dependency
	modeDepUnlink             // pick existing dependency to remove
)

type pickerIntent int

const (
	pickerManage pickerIntent = iota // manage existing item's project membership
	pickerCreate                     // select projects for new item
)

type filterMode int

const (
	filterNone    filterMode = iota
	filterBlocked            // show only blocked items
	filterAll                // include archived items
)
