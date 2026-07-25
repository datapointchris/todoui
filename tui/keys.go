package tui

type pane int

const (
	projectPane pane = iota
	itemPane
)

type appMode int

const (
	modeNormal          appMode = iota
	modeAddItem                 // text input: new item in current project
	modeAddItemMulti            // text input → project picker: multi-project add
	modeAddProject              // text input: new project
	modeAddTask                 // text input: new sub-task on the item pane's item
	modeEditTitle               // text input: edit existing item title
	modeProjectPicker           // checkbox picker: manage project membership
	modeItemDetail              // expanded item view (read-only with action keys)
	modeEditNotes               // multiline textarea for notes editing
	modeHelp                    // keybinding reference overlay
	modeSearch                  // search text input + results
	modeMove                    // grab item, j/k to reposition
	modeMoveProject             // grab project, j/k to reposition
	modeDepLink                 // pick item to add as dependency
	modeDepUnlink               // pick existing dependency to remove
	modeProjectDetail           // expanded project view (read-only with action keys)
	modeEditProjectName         // text input: edit project name
	modeEditProjectDesc         // multiline textarea for project description editing
)

// rowKind identifies what a navigable row in the item pane represents.
// The item pane presents items and their sub-tasks as one unified list;
// rowKind tells the renderer and key handlers how to treat each row.
type rowKind int

const (
	rowItem rowKind = iota
	rowTask
)

// paneRow is one navigable row in the item pane's unified cursor space.
// itemIdx is always the owning item's index in m.items. taskIdx is the
// task's index in m.itemTasks[item.ID] when kind == rowTask, else -1.
type paneRow struct {
	kind    rowKind
	itemIdx int
	taskIdx int
}

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
