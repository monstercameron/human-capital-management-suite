package journey

import (
	"regexp"
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_005_JourneyInteractiveColorsStayScoped(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`body:has(>.jn-page){margin:0;}`,
		`:where(.jn-page,.jn-embedded){background-color:var(--jn-canvas);`,
		`:where(.jn-page,.jn-embedded) :is(h1,h2,h3,h4,p,ul,ol,dl,dd,figure,table){margin:0;}`,
		`:where(.jn-page,.jn-embedded) svg{display:block;}`,
		`:where(.jn-page,.jn-embedded) h1{`,
		`:where(.jn-page,.jn-embedded) h2{`,
		`:where(.jn-page,.jn-embedded) h3{`,
		`:where(.jn-page,.jn-embedded) :is(img,svg,video,canvas){max-width:100%;}`,
		`:where(.jn-page,.jn-embedded) :is(input,select,textarea,button){max-width:100%;}`,
		`:where(.jn-page,.jn-embedded) a{color:var(--jn-accent);`,
		`:where(.jn-page,.jn-embedded) a:hover{color:var(--jn-accent-strong);`,
		`:where(.jn-page,.jn-embedded) :focus-visible{`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("embedded journey lost scoped interactive rule %q", want)
		}
	}
	for _, leaked := range []string{
		`(?:^|})body\{`, `(?:^|})h1,h2,h3,h4,p,ul,ol,dl,dd,figure,table\{`,
		`(?:^|})h1\{`, `(?:^|})h2\{`, `(?:^|})h3\{`, `(?:^|})img,svg,video,canvas\{`,
		`(?:^|})svg\{`, `(?:^|})a\{`, `(?:^|})a:hover\{`, `(?:^|}):focus-visible\{`,
		`(?:^|})\*,\*::before,\*::after\{`,
	} {
		if regexp.MustCompile(leaked).MatchString(css) {
			t.Errorf("journey stylesheet has unscoped interactive rule %q", leaked)
		}
	}
}
