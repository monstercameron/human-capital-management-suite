package chat

// AssigneeOnlyGap is the place of the first item of a to-do list that nobody
// could complete: the list lets only the assignee complete items and the item
// has no assignee. It is -1 when every item has someone who may (#3923). The
// rule is for lists being posted; a card stored earlier is still read as it was.
func (c Chatcmd002Card) AssigneeOnlyGap() int {
	if c.Todo == nil || c.Todo.Tick != "assignee" {
		return -1
	}
	for i, item := range c.Todo.Items {
		if item.AssigneeID == "" {
			return i
		}
	}
	return -1
}
