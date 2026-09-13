package extract

// State pairs a two-letter postal code with the research file that describes
// it. The list is the extractor's iteration order, so it is sorted by code
// and never derived from a map.
type State struct {
	Code string
	Name string
	File string
}

// States is the fifty-one rows of the contract's section 5, each with the
// research file its citations point back to.
var States = []State{
	{"AK", "Alaska", "alaska.md"},
	{"AL", "Alabama", "alabama.md"},
	{"AR", "Arkansas", "arkansas.md"},
	{"AZ", "Arizona", "arizona.md"},
	{"CA", "California", "california.md"},
	{"CO", "Colorado", "colorado.md"},
	{"CT", "Connecticut", "connecticut.md"},
	{"DC", "District of Columbia", "district-of-columbia.md"},
	{"DE", "Delaware", "delaware.md"},
	{"FL", "Florida", "florida.md"},
	{"GA", "Georgia", "georgia.md"},
	{"HI", "Hawaii", "hawaii.md"},
	{"IA", "Iowa", "iowa.md"},
	{"ID", "Idaho", "idaho.md"},
	{"IL", "Illinois", "illinois.md"},
	{"IN", "Indiana", "indiana.md"},
	{"KS", "Kansas", "kansas.md"},
	{"KY", "Kentucky", "kentucky.md"},
	{"LA", "Louisiana", "louisiana.md"},
	{"MA", "Massachusetts", "massachusetts.md"},
	{"MD", "Maryland", "maryland.md"},
	{"ME", "Maine", "maine.md"},
	{"MI", "Michigan", "michigan.md"},
	{"MN", "Minnesota", "minnesota.md"},
	{"MO", "Missouri", "missouri.md"},
	{"MS", "Mississippi", "mississippi.md"},
	{"MT", "Montana", "montana.md"},
	{"NC", "North Carolina", "north-carolina.md"},
	{"ND", "North Dakota", "north-dakota.md"},
	{"NE", "Nebraska", "nebraska.md"},
	{"NH", "New Hampshire", "new-hampshire.md"},
	{"NJ", "New Jersey", "new-jersey.md"},
	{"NM", "New Mexico", "new-mexico.md"},
	{"NV", "Nevada", "nevada.md"},
	{"NY", "New York", "new-york.md"},
	{"OH", "Ohio", "ohio.md"},
	{"OK", "Oklahoma", "oklahoma.md"},
	{"OR", "Oregon", "oregon.md"},
	{"PA", "Pennsylvania", "pennsylvania.md"},
	{"RI", "Rhode Island", "rhode-island.md"},
	{"SC", "South Carolina", "south-carolina.md"},
	{"SD", "South Dakota", "south-dakota.md"},
	{"TN", "Tennessee", "tennessee.md"},
	{"TX", "Texas", "texas.md"},
	{"UT", "Utah", "utah.md"},
	{"VA", "Virginia", "virginia.md"},
	{"VT", "Vermont", "vermont.md"},
	{"WA", "Washington", "washington.md"},
	{"WI", "Wisconsin", "wisconsin.md"},
	{"WV", "West Virginia", "west-virginia.md"},
	{"WY", "Wyoming", "wyoming.md"},
}

// StateByCode returns the row for a postal code.
func StateByCode(code string) (State, bool) {
	for _, s := range States {
		if s.Code == code {
			return s, true
		}
	}
	return State{}, false
}
