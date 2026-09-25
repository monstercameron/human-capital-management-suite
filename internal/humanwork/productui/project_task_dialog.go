package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// projectTaskDialogProps configures the ticket modal over the board.
type projectTaskDialogProps struct {
	TaskID string
	Label  string
	Body   ui.Node
	Close  func()
}

const projectTaskDialogID = "project-task-dialog"

// projectTaskDialog is the ticket modal. The address owns whether it is
// open (the board's task= selector), so the component only mirrors that:
// on mount it promotes the native <dialog> to a modal (the browser supplies
// the focus trap, the inert page and Escape), and on close it returns focus
// to the card that opened it. Escape, the backdrop and the close button all
// call Close, which leaves the task selector through history.
func projectTaskDialog(props projectTaskDialogProps) ui.Node {
	useProjectTaskDialog(projectTaskDialogID, props.TaskID, props.Close)
	return html.Tag("dialog", html.Props{ID: projectTaskDialogID, Class: "project-task-dialog", TabIndex: -1, Aria: map[string]string{"labelledby": "project-task-dialog-title", "label": props.Label}, Data: map[string]string{"task-id": props.TaskID}}, props.Body)
}

// projectLastBoardHref remembers the board address rendered just before a
// task modal opened, so closing it can step Back instead of adding a new
// history entry when the modal was opened from that board.
var projectLastBoardHref string
var projectModalFromBoard bool

// projectNoteBoardShown records whether the modal about to render was opened
// from the board already on screen. It is presentation bookkeeping only.
func projectNoteBoardShown(boardHref string, modalOpen bool) {
	if !modalOpen {
		projectLastBoardHref, projectModalFromBoard = boardHref, false
		return
	}
	if projectLastBoardHref == boardHref && boardHref != "" {
		projectModalFromBoard = true
	}
	projectLastBoardHref = ""
}

// projectCloseTaskDialog leaves the task selector: Back when the modal was
// opened from this board in this session, otherwise a replace to the board
// so a shared link closes to its board without trapping Back.
func projectCloseTaskDialog(view View, boardHref string) {
	if projectModalFromBoard && view.HistoryNavigation.CanGoBack && view.HistoryNavigation.GoBack != nil {
		projectModalFromBoard = false
		view.HistoryNavigation.GoBack()
		return
	}
	projectModalFromBoard = false
	switch {
	case view.NavigateReplace != nil:
		view.NavigateReplace(boardHref)
	case view.Navigate != nil:
		view.Navigate(boardHref)
	}
}
