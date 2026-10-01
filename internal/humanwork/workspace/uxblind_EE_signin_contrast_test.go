package workspace

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestTodo_UXBLIND_086(t *testing.T) {
	sheet := loginCompanyStylesheet()
	for _, want := range []string{
		`.company-option .company-name`,
		`.company-option .company-description`,
		"17231d", "34443a", "f2f0ec", "b5b1aa", "fdeee5", "2b2023",
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("company contrast stylesheet missing %q", want)
		}
	}

	for _, colors := range [][2]string{
		{"17231d", "fbfcfb"}, // HarborCare, light, unselected.
		{"34443a", "fbfcfb"},
		{"17231d", "fdeee5"}, // Ironridge, light, selected.
		{"34443a", "fdeee5"},
		{"f2f0ec", "1b1e22"}, // Any card, dark, unselected.
		{"b5b1aa", "1b1e22"},
		{"f2f0ec", "2b2023"}, // Ironridge, dark, selected.
		{"b5b1aa", "2b2023"},
	} {
		if ratio := contrastRatio(colors[0], colors[1]); ratio < 4.5 {
			t.Errorf("contrast %s on %s = %.2f, want at least 4.5", colors[0], colors[1], ratio)
		}
	}
}

func TestTodo_UXBLIND_086_Browser(t *testing.T) {
	h := newCompanyLoginHandler(t)
	_, harbor := getCompanyLoginPage(t, h, PathLogin)
	_, ironridge := getCompanyLoginPage(t, h, PathLogin+"?company=ironridge-demo")

	for name, body := range map[string]string{"HarborCare": harbor, "Ironridge": ironridge} {
		for _, want := range []string{`class="company-option"`, `class="company-name"`, `class="company-description"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s sign-in page missing %q", name, want)
			}
		}
	}
	if !strings.Contains(harbor, `data-company="harborcare-demo" aria-current="true"`) {
		t.Fatal("HarborCare card is not selected on the default page")
	}
	if !strings.Contains(ironridge, `data-company="ironridge-demo" aria-current="true"`) ||
		!strings.Contains(ironridge, `body[data-hcm-company="ironridge-demo"] .company-option[aria-current="true"]`) {
		t.Fatal("Ironridge selected card lost its themed contrast rules")
	}
}

func TestTodo_UXBLIND_086_Regression(t *testing.T) {
	h := newDirectoryHandler(t)
	if body := getLoginPage(t, h, ""); strings.Contains(body, `class="company-option"`) {
		t.Fatal("single-company sign-in page acquired company-card markup")
	}
	if sheet := loginStylesheet(); strings.Contains(sheet, ".company-option .company-name") {
		t.Fatal("single-company login stylesheet acquired multi-company text overrides")
	}

	companies := []DevCompany{
		{Key: "harborcare-demo", Name: "HarborCare", Description: "Community care"},
		{Key: "ironridge-demo", Name: "Ironridge", Description: "Commercial construction"},
	}
	markup := companySelector(companies, companies[0], "en-US")
	if !strings.Contains(markup, `aria-current="true"`) || strings.Contains(markup, `style=`) {
		t.Fatal("company selector lost native selection markup or gained inline styling")
	}
}

func contrastRatio(foreground, background string) float64 {
	foregroundLuminance := colorLuminance(foreground)
	backgroundLuminance := colorLuminance(background)
	if foregroundLuminance < backgroundLuminance {
		foregroundLuminance, backgroundLuminance = backgroundLuminance, foregroundLuminance
	}
	return (foregroundLuminance + 0.05) / (backgroundLuminance + 0.05)
}

func colorLuminance(value string) float64 {
	value = strings.TrimPrefix(value, "#")
	var channels [3]float64
	for index := range channels {
		channel, err := strconv.ParseUint(value[index*2:index*2+2], 16, 8)
		if err != nil {
			panic(err)
		}
		normalized := float64(channel) / 255
		if normalized <= 0.03928 {
			channels[index] = normalized / 12.92
		} else {
			channels[index] = math.Pow((normalized+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
}
