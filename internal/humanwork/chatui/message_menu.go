package chatui

func messageMenuNextIndex(key string, current, length int) int {
	if length <= 0 {
		return -1
	}
	switch key {
	case "ArrowDown":
		return (current + 1) % length
	case "ArrowUp":
		if current < 0 {
			return length - 1
		}
		return (current + length - 1) % length
	case "Home":
		return 0
	case "End":
		return length - 1
	default:
		return current
	}
}
