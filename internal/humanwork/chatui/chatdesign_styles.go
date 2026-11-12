package chatui

// ChatDesignStyles is the Chat design pass (#3769 to #3777): one visual
// direction laid over the existing structure. It is appended last to
// Stylesheet, so a rule here wins over an earlier rule of equal specificity.
const ChatDesignStyles = chatDesignAccentStyles + chatDesignAvatarStyles + chatDesignUnreadStyles

// chatDesignAccentStyles declares the accent colour for TEXT (#3769). The brand
// primary is a fill: as text on the dark message and card surfaces it reads at
// about 2.6:1. --accent is the theme-resolved text-safe accent (the brand colour
// in light, its lightened mix in dark, the contrast and forced-colours values
// where those apply), so every accent-coloured link in Chat reads this token.
const chatDesignAccentStyles = `.chat-workspace{--chat-accent-text:var(--accent)}`
