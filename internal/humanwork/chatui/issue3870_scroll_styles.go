package chatui

// Issue3870Styles keeps section shortcuts clear of the sticky header. The side
// column scrolls under a 52px sticky heading, so a jump that starts a section at
// the top edge (Pinned, Members) parked its title behind the header. The padding
// is the scrollport's, which scrollIntoView honours, and is the header's height
// plus a gap, so the focused heading is fully visible.
const Issue3870Styles = `.chat-side{scroll-padding-top:64px}`
