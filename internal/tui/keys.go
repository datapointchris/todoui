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
)

type pickerIntent int

const (
	pickerManage pickerIntent = iota // manage existing item's project membership
	pickerCreate                     // select projects for new item
)
