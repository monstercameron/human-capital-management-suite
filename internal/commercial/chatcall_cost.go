package commercial

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Call media planning rates. Money is integer cents; per-minute vendor
// prices are per thousand minutes to keep the arithmetic exact. Every rate
// is a stated assumption, re-quoted before anything is built or bought.
const (
	CallAudioKBPS = 24
	CallVideoKBPS = 1000
	// ManagedAudioCentsPer1000Min and ManagedVideoCentsPer1000Min are the
	// assumed managed-service prices per participant minute ($2 and $8 per
	// thousand minutes).
	ManagedAudioCentsPer1000Min = 200
	ManagedVideoCentsPer1000Min = 800
	// SelfHostFixedCentsPerMonth is the assumed relay plus forwarding-unit
	// fleet before bandwidth ($600).
	SelfHostFixedCentsPerMonth = 60000
	// EgressCentsPerGB is the assumed egress price ($0.09/GB).
	EgressCentsPerGB = 9
)

// CallCostAssumptions states the usage the scenarios are costed for.
type CallCostAssumptions struct {
	MinutesPerPersonPerMonth int
	VideoSharePct            int
}

// CallCostUsage returns the assumed usage: sixty call minutes per person
// per month, one quarter of them video, the rest voice.
func CallCostUsage() CallCostAssumptions {
	return CallCostAssumptions{MinutesPerPersonPerMonth: 60, VideoSharePct: 25}
}

func callAudioMinutes(seats int) int64 {
	use := CallCostUsage()
	return int64(seats) * int64(use.MinutesPerPersonPerMonth) * int64(100-use.VideoSharePct) / 100
}

func callVideoMinutes(seats int) int64 {
	use := CallCostUsage()
	return int64(seats) * int64(use.MinutesPerPersonPerMonth) * int64(use.VideoSharePct) / 100
}

// ManagedMonthlyCents is the assumed managed-service bill: per-participant
// minutes at the pinned rates, no fleet to run.
func ManagedMonthlyCents(seats int) int64 {
	return callAudioMinutes(seats)*ManagedAudioCentsPer1000Min/1000 +
		callVideoMinutes(seats)*ManagedVideoCentsPer1000Min/1000
}

// SelfHostMonthlyCents is the assumed self-hosted bill: the fixed fleet
// plus egress for every relayed minute. The relay-only default of
// CHATCALL-003 means every call crosses the relay, so all media is egress.
func SelfHostMonthlyCents(seats int) int64 {
	audioBytes := callAudioMinutes(seats) * 60 * CallAudioKBPS * 1000 / 8
	videoBytes := callVideoMinutes(seats) * 60 * CallVideoKBPS * 1000 / 8
	return SelfHostFixedCentsPerMonth + (audioBytes+videoBytes)*EgressCentsPerGB/1000000000
}

// FlipSeats is the smallest customer size, in seats, at which the model
// favours self-hosting over the managed service.
func FlipSeats() int {
	for seats := 1; seats <= 100000; seats++ {
		if SelfHostMonthlyCents(seats) < ManagedMonthlyCents(seats) {
			return seats
		}
	}
	return -1
}

// CallCostScenario is one customer size with both monthly bills in cents.
type CallCostScenario struct {
	Seats                 int
	SelfHostCentsPerMonth int64
	ManagedCentsPerMonth  int64
}

// CallCost is the CHATCALL-006 record: the model, the comparison, the
// operating and commercial consequences, and the recommendation.
type CallCost struct {
	Assumptions           CallCostAssumptions
	Scenarios             []CallCostScenario
	FlipSeats             int
	Recommendation        string
	OpsWork               string
	Residency             string
	PrivateNetwork        string
	LockIn                string
	EncryptionConstraints string
	QualityMeasures       []string
	OperatorSurface       string
}

// CallCostRecord returns the record.
func CallCostRecord() CallCost {
	use := CallCostUsage()
	scenarios := make([]CallCostScenario, 0, 3)
	for _, seats := range []int{50, 500, 5000} {
		scenarios = append(scenarios, CallCostScenario{
			Seats:                 seats,
			SelfHostCentsPerMonth: SelfHostMonthlyCents(seats),
			ManagedCentsPerMonth:  ManagedMonthlyCents(seats),
		})
	}
	_ = use
	return CallCost{
		Assumptions:           use,
		Scenarios:             scenarios,
		FlipSeats:             FlipSeats(),
		Recommendation:        "Buy the forwarding unit managed, run the relay at home, and re-platform the forwarding unit past the flip volume. The relay (coturn) is cheap, stateless and residency-critical, so it is self-operated from day one under the deployment's own region rules. The forwarding unit starts managed, because a fifty-person customer cannot amortise a media fleet, and moves to a self-hosted unit once call minutes cross the flip volume the model reports. Signalling stays vendor-neutral per CHATCALL-003, so the switch touches media servers only.",
		OpsWork:               "Self-hosting is a different on-call from the application server: relay and forwarding-unit capacity per region with headroom for the morning peak, the four quality measures below plus server health on every dashboard, LiveKit and coturn patch cadence with zero-downtime upgrades, and media incident response with region failover rehearsed. Managed, the vendor carries capacity, upgrades and media incidents; the product still owns integration health, cost monitoring against the model, and vendor-incident coordination with customer advisories.",
		Residency:             "Self-hosted, media stays in the tenant's region under the deployment's own residency rules. Managed, the media vendor becomes a new sub-processor: it joins the sub-processor registry with its regions, data-processing terms, data classes and retention, and the egress profile terms (allowed regions, allowed classes, retention ceiling) must cover call audio and video before any media leaves the deployment. A tenant whose profile admits no outside region cannot use a managed unit outside it.",
		PrivateNetwork:        "A tenant on a private network reaches an in-region relay over TURN/TLS on port 443 and a forwarding unit in its own region or VPC. Self-hosting serves that tenant directly; a managed service serves it only where the vendor offers private connectivity or an in-region unit — otherwise the tenant gets no calls, and the product must say so at sale time rather than at ring time.",
		LockIn:                "Self-hosted, the media servers are open source (Pion, LiveKit, coturn): exit is re-deployment, and the cost of exit is the fleet. Managed, the pricing is per minute and the APIs are the vendor's: exit needs a commercial exit-terms analogue — metadata export, a deletion certificate, no retained media — plus a re-platform plan to the self-hosted unit. Either way the exit document exists before the first paid call minute.",
		EncryptionConstraints: "The choice constrains the CHATCALL-005 answers. Self-hosted keeps them intact: end-to-end one-to-one calls terminate nowhere, and any future server recording or transcription lives under the conversation's own governance. Managed, one-to-one end-to-end survives only if the vendor never needs plaintext; any vendor-side recording, captioning or transcription puts call content in the vendor's hands under its data-processing terms and retention, and the strictest-wins consent of CHATCALL-005 must be enforceable through the vendor's controls, not just ours.",
		QualityMeasures:       []string{"setup success rate", "time to first audio", "drop rate", "audio quality score"},
		OperatorSurface:       "Whichever option is chosen, the four measures are watched in the operations explorer with SLO burn-down, alerted through telemetry, and raised as incidents with the same incident state as any other outage. A call-quality regression pages like a downtime regression.",
	}
}

func formatSeats(seats int) string {
	if seats == 5000 {
		return "5,000"
	}
	return fmt.Sprintf("%d", seats)
}

func formatCents(cents int64) string {
	return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
}

// RenderCallCost is the stable text form used by the golden test.
func RenderCallCost() string {
	r := CallCostRecord()
	var b strings.Builder
	b.WriteString("CHATCALL-006 build against buy recorded 2026-10-22\n")
	fmt.Fprintf(&b, "\nassumes %d call minutes per person per month, %d%% video\n",
		r.Assumptions.MinutesPerPersonPerMonth, r.Assumptions.VideoSharePct)
	b.WriteString("\nscenarios (monthly):\n")
	for _, s := range r.Scenarios {
		fmt.Fprintf(&b, "%s seats: self-host %s, managed %s\n",
			formatSeats(s.Seats), formatCents(s.SelfHostCentsPerMonth), formatCents(s.ManagedCentsPerMonth))
	}
	fmt.Fprintf(&b, "\nflip volume: %d seats\nrecommendation: %s\n", r.FlipSeats, r.Recommendation)
	fmt.Fprintf(&b, "\noperating work: %s\nresidency: %s\nprivate network: %s\nlock-in and exit: %s\nencryption constraints: %s\n",
		r.OpsWork, r.Residency, r.PrivateNetwork, r.LockIn, r.EncryptionConstraints)
	fmt.Fprintf(&b, "\nquality: %s\noperators watch: %s\n",
		strings.Join(r.QualityMeasures, ", "), r.OperatorSurface)
	return b.String()
}

// CallCostDigest is the digest of the rendered comparison.
func CallCostDigest() string {
	sum := sha256.Sum256([]byte(RenderCallCost()))
	return hex.EncodeToString(sum[:])
}
