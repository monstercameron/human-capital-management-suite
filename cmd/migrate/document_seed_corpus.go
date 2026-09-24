package main

// The document seed's corpus: the titles a HarborCare employee library holds
// and the Markdown each one renders. Everything here is a pure function of
// the plan's seeded random source, so a seed run is reproducible.

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// Document kinds; the viewer's folders file by kind.
const (
	kindPolicy     = "policy"
	kindOnboarding = "onboarding"
	kindRunbook    = "runbook"
	kindNotes      = "notes"
	kindFAQ        = "faq"
	kindShiftSwap  = "shiftswap"
	kindIncident   = "incident"
	kindComp       = "comp"
	kindGuide      = "guide"
	kindClinical   = "clinical"
	kindEngineer   = "engineering"
	kindOps        = "operations"
)

// Owning domains, matched against journey_worker org units.
const (
	domainPeople    = "people"
	domainClinical  = "clinical"
	domainTech      = "tech"
	domainFinance   = "finance"
	domainLegal     = "legal"
	domainWorkplace = "workplace"
	domainGTM       = "gtm"
	domainExec      = "exec"
)

var domainUnits = map[string][]string{
	domainPeople:    {"people-operations"},
	domainClinical:  {"clinical-operations", "quality-safety", "care-coordination"},
	domainTech:      {"engineering-platform", "security-it", "data-analytics", "product-management"},
	domainFinance:   {"finance"},
	domainLegal:     {"legal-compliance"},
	domainWorkplace: {"workplace-services"},
	domainGTM:       {"sales", "marketing", "customer-success"},
	domainExec:      {"executive-office"},
}

// seedSpec is one catalog entry before an owner, age or audience is chosen.
type seedSpec struct {
	Kind, Domain, Title, Topic string
	// Dated specs put their date in the title; Age is then fixed by the plan.
	Dated bool
	// Table marks the few documents whose body carries a Markdown table.
	Table bool
}

type policyTopic struct {
	Name, Domain, Purpose string
	Rules                 []string
}

var seedPolicies = []policyTopic{
	{"Paid time off", domainPeople, "Explains how paid time off accrues, how to request it and how balances carry over.", []string{"Full-time staff accrue 6.15 hours per biweekly pay period in the first two years of service.", "Requests of three days or more need two weeks' notice; your manager responds within three business days.", "Up to 40 hours carry over into the next calendar year; anything above that is forfeited on January 1.", "Clinical units may restrict time off during the holiday blackout window published each September."}},
	{"Sick leave", domainPeople, "Sets out paid sick leave for employees, their family members and safe-time needs.", []string{"Sick leave accrues at one hour for every 30 hours worked, with no annual cap on use.", "No doctor's note is required for absences of three consecutive shifts or fewer.", "Sick leave may be used for a family member's illness, preventive care or safe time.", "Clinical staff call the staffing line at least two hours before the start of a shift."}},
	{"Parental leave", domainPeople, "Describes paid leave for birth, adoption and foster placement.", []string{"Birthing parents receive 16 weeks of fully paid leave; non-birthing parents receive 12 weeks.", "Leave may be taken in up to two blocks within 12 months of the birth or placement.", "State paid family leave runs concurrently; HarborCare tops up to full base pay.", "Benefits continue unchanged throughout the leave."}},
	{"Bereavement leave", domainPeople, "Gives employees paid time to grieve and handle arrangements after a loss.", []string{"Up to five paid days for the loss of an immediate family member and three days for extended family.", "Days do not need to be consecutive and may be used within 60 days of the loss.", "Managers approve bereavement requests without asking for documentation."}},
	{"Jury duty and civic leave", domainPeople, "Covers time away for jury service, voting and serving as a witness.", []string{"Employees receive full base pay for up to 10 days of jury service per year.", "Night-shift staff summoned for day jury service are released from the preceding shift.", "Up to two paid hours are available to vote when polls are not open outside scheduled hours."}},
	{"Remote and hybrid work", domainPeople, "Defines which roles may work remotely and what we expect of remote work.", []string{"Roles are classified as on-site, hybrid (two to three office days) or remote in the job description.", "Remote employees must work from a state where HarborCare is registered as an employer.", "Patient information may only be accessed on managed devices over the company VPN.", "Hybrid teams agree on shared anchor days and publish them in the team charter."}},
	{"Travel and expense", domainFinance, "Sets the rules for booking business travel and claiming reimbursement.", []string{"Book flights and hotels through the travel portal at least 14 days ahead where possible.", "Meals are reimbursed up to the per-diem rate for the destination city.", "Submit expense reports within 30 days of the trip with itemized receipts over $25.", "Alcohol, upgrades and personal entertainment are not reimbursable."}},
	{"Overtime and premium pay", domainPeople, "Explains when overtime is paid and how premium rates stack.", []string{"Non-exempt employees receive 1.5 times their regular rate for hours over 40 in a workweek.", "Where state law requires daily overtime, the more generous rule applies.", "Overtime must be approved in advance by the charge nurse or manager, except in patient emergencies.", "Premiums are calculated on the regular rate including shift differentials."}},
	{"On-call compensation", domainPeople, "Describes pay for employees who carry a pager or phone outside scheduled hours.", []string{"On-call staff receive $4.00 per hour of on-call time, rising to $6.00 on holidays.", "Call-back time is paid at the overtime rate with a two-hour minimum.", "Response time expectations are set per team and published in the on-call handbook."}},
	{"Night shift differential", domainPeople, "Sets the differential paid for evening, night and weekend shifts.", []string{"Evening shifts (3 p.m. to 11 p.m.) earn a $2.50 hourly differential.", "Night shifts (11 p.m. to 7 a.m.) earn a $4.25 hourly differential.", "Weekend shifts earn an additional $1.75 per hour, stacked on the shift differential.", "Differentials are included in the regular rate for overtime."}},
	{"Holiday pay", domainPeople, "Lists observed holidays and how holiday work is paid.", []string{"HarborCare observes the eight holidays listed below.", "Employees who work a holiday shift receive 1.5 times their base rate for hours worked.", "Part-time staff receive holiday pay pro-rated to their scheduled FTE."}},
	{"Code of conduct", domainLegal, "States the standards of behavior every HarborCare employee and contractor commits to.", []string{"Treat patients, colleagues and partners with respect and dignity.", "Protect patient and employee information as if it were your own.", "Report concerns through your manager, People Operations or the confidential ethics line.", "Retaliation against anyone who raises a concern in good faith is prohibited."}},
	{"Anti-harassment and discrimination", domainLegal, "Prohibits harassment and discrimination and explains how to report it.", []string{"Harassment based on any protected characteristic is prohibited at work and at work-related events.", "Reports can be made to any manager, People Partner or the ethics line, anonymously if preferred.", "Every report is reviewed within two business days and investigated promptly.", "Supervisors must report any harassment they observe or are told about."}},
	{"Workplace violence prevention", domainClinical, "Sets expectations for preventing and responding to violence in clinical and office settings.", []string{"Threats or acts of violence are never tolerated, from employees, patients or visitors.", "Call a Code Grey for a combative person and a Code Silver for a person with a weapon.", "All incidents, including verbal threats, are reported in the safety event system.", "Staff in emergency and behavioral health units complete de-escalation training annually."}},
	{"Drug and alcohol testing", domainLegal, "Explains when drug and alcohol testing happens and what the results mean.", []string{"Post-offer testing applies to all clinical and safety-sensitive roles.", "Reasonable-suspicion testing requires two trained observers to document the behavior.", "Employees may self-refer to the EAP before a policy violation without discipline."}},
	{"Immunization requirements", domainClinical, "Lists the immunizations required for staff who work in or visit clinical areas.", []string{"Clinical staff must show proof of MMR, varicella, hepatitis B and Tdap immunity before their first shift.", "Annual influenza vaccination is due by November 15.", "Medical and religious exemptions are reviewed by Employee Health within ten business days.", "Exempt staff wear a surgical mask in patient areas during flu season."}},
	{"Background check", domainPeople, "Describes pre-employment and periodic background screening.", []string{"All offers are contingent on a background check that meets state and federal requirements.", "Clinical staff are re-screened against the OIG exclusion list monthly.", "Candidates receive a copy of the report and time to respond before any adverse decision."}},
	{"Social media", domainGTM, "Guides employees on personal and professional social media use.", []string{"Never post patient information or images, even with names removed.", "Make clear that personal posts reflect your own views, not HarborCare's.", "Refer media inquiries to the communications team."}},
	{"Conflict of interest", domainLegal, "Helps employees recognize and disclose conflicts of interest.", []string{"Disclose any financial interest in a vendor, competitor or referral source.", "Complete the annual disclosure attestation by March 31.", "Do not take part in a purchasing decision involving a disclosed interest."}},
	{"Gifts and entertainment", domainLegal, "Sets limits on gifts to and from vendors, patients and partners.", []string{"Employees may accept gifts worth $50 or less, and no more than $150 from one source per year.", "Cash and gift cards are never accepted.", "Patient thank-you gifts are shared with the unit where possible."}},
	{"Whistleblower protection", domainLegal, "Protects employees who report suspected wrongdoing.", []string{"Concerns may be raised internally or with a regulator at any time.", "The ethics line is operated by an independent provider and accepts anonymous reports.", "Retaliation is a serious violation and results in discipline up to termination."}},
	{"Attendance and tardiness", domainPeople, "Sets expectations for reliable attendance, especially on staffed units.", []string{"Notify your manager or the staffing line at least two hours before an unplanned absence.", "Clock in no earlier than seven minutes before your scheduled start.", "Patterns of unplanned absence are addressed through coaching before progressive discipline.", "Protected leave is never counted as an occurrence."}},
	{"Dress code and uniforms", domainClinical, "Describes uniform and dress expectations by work area.", []string{"Clinical staff wear the scrub color assigned to their discipline.", "Badges are worn above the waist with the photo visible at all times.", "Artificial nails are not permitted for staff with direct patient contact."}},
	{"Personal device use", domainTech, "Explains how personal phones and laptops may be used for work.", []string{"Personal phones may be used for email and chat only after enrolling in device management.", "Patient information must never be stored on a personal device.", "Lost or stolen devices are reported to the IT service desk within one hour."}},
	{"Tuition reimbursement", domainPeople, "Supports employees continuing their education.", []string{"Employees with six months of service may claim up to $5,250 per calendar year.", "Courses must relate to a current or future HarborCare role and be approved before enrollment.", "Reimbursement requires a grade of B or better, or a pass in pass/fail courses.", "Employees who leave within 12 months repay a pro-rated share."}},
	{"Employee referral bonus", domainPeople, "Rewards employees who refer successful hires.", []string{"Referral bonuses are $1,500 for most roles and $3,000 for registered nurses and engineers.", "The bonus is paid after the new hire completes 90 days of employment.", "Hiring managers and recruiters are not eligible for referrals into their own openings."}},
	{"Internal transfer", domainPeople, "Explains how employees move between teams and roles.", []string{"Employees in good standing with nine months in role may apply for internal openings.", "Tell your current manager before a second-round interview.", "Transfers take effect within four weeks of acceptance unless both managers agree otherwise."}},
	{"Rehire eligibility", domainPeople, "Sets out who may be rehired and on what terms.", []string{"Former employees who left in good standing are eligible for rehire.", "Rehires within 12 months keep their original service date for PTO accrual.", "Eligibility is recorded at separation and reviewed by People Operations."}},
	{"Progressive discipline", domainPeople, "Describes the steps used to address performance and conduct concerns.", []string{"Steps are coaching, written warning, final written warning and termination.", "Serious misconduct may skip steps after review by a People Partner.", "Every step is documented and shared with the employee."}},
	{"Performance improvement plans", domainPeople, "Explains when and how a performance improvement plan is used.", []string{"A plan runs for 30, 60 or 90 days with clear, measurable goals.", "Managers meet the employee weekly and document progress.", "A People Partner reviews every plan before it is delivered."}},
	{"Lactation accommodation", domainPeople, "Supports employees who need to express milk at work.", []string{"Reasonable break time is provided for up to two years after birth.", "Private lactation rooms are available in every building; see the facilities guide.", "Break time beyond paid rest periods may be unpaid for non-exempt staff."}},
	{"Reasonable accommodation", domainPeople, "Explains how to request a workplace accommodation.", []string{"Requests may be made verbally or in writing to a manager or People Partner.", "People Operations begins the interactive process within five business days.", "Medical information is kept separate from the personnel file."}},
	{"Military leave", domainPeople, "Protects employees who serve in the uniformed services.", []string{"Employees receive job-protected leave for military service under USERRA.", "HarborCare pays the difference between military and base pay for up to 30 days a year.", "Benefits continue during leave of 30 days or less."}},
	{"Inclement weather", domainWorkplace, "Explains how operations continue during severe weather.", []string{"Hospitals and clinics stay open; essential clinical staff report as scheduled.", "Office staff work remotely when the building closure alert is sent.", "Staff who stay overnight for coverage receive lodging and meals."}},
	{"Timekeeping and meal breaks", domainPeople, "Sets rules for recording time and taking breaks.", []string{"Non-exempt staff record all time worked, including pre-shift huddles.", "A 30-minute unpaid meal break is scheduled for shifts over six hours.", "Missed meal breaks are reported the same day so a premium can be paid."}},
	{"Credentialing and license verification", domainClinical, "Ensures clinical staff hold current licenses and certifications.", []string{"Licenses are verified at hire and at each renewal through the state board.", "BLS certification must remain current; ACLS is required on critical care units.", "Staff with lapsed credentials are removed from the schedule until renewed."}},
	{"Floating and cross-unit assignment", domainClinical, "Explains how nurses are floated to balance staffing.", []string{"Floating follows the rotation list kept by the house supervisor.", "Nurses float only to units where they have completed a competency checklist.", "No nurse floats more than twice in a pay period without their agreement."}},
	{"Moonlighting and secondary employment", domainPeople, "Sets expectations for employees who hold a second job.", []string{"Secondary employment is allowed if it does not conflict with scheduled shifts or create a conflict of interest.", "Clinical staff must observe the 16-hour maximum across all employers.", "Disclose secondary employment with a competitor or vendor."}},
	{"Relocation assistance", domainPeople, "Describes support for employees relocating for a role.", []string{"Eligible hires receive a lump sum of $7,500 or managed moving services.", "Temporary housing is covered for up to 30 days.", "Relocation is repaid on a pro-rated basis if the employee leaves within 12 months."}},
	{"Sabbatical", domainPeople, "Rewards long service with an extended paid break.", []string{"Employees with seven years of continuous service may take a four-week paid sabbatical.", "Sabbaticals are scheduled at least three months ahead with manager approval.", "Sabbatical time does not reduce PTO balances."}},
}

var seedOnboardingRoles = []string{
	"Registered nurse", "Nurse practitioner", "Care coordinator", "Software engineer", "Site reliability engineer",
	"Data analyst", "Product manager", "Account executive", "Customer success manager", "People partner",
	"Accountant", "Security engineer", "IT systems administrator", "Workplace coordinator", "Patient safety specialist",
}

var seedOnboardingGuides = []string{
	"Manager onboarding: the first 90 days", "New hire orientation agenda", "Welcome to HarborCare: your first week",
	"Onboarding buddy program guide", "Pre-boarding email templates", "I-9 and E-Verify onboarding steps",
	"Equipment request guide for new hires", "Clinical orientation schedule: spring cohort", "Clinical orientation schedule: fall cohort",
	"Onboarding survey results: first half 2026",
}

var seedRunbooks = []struct{ System, Domain, Symptom string }{
	{"Payroll close", domainFinance, "the biweekly payroll must be finalized and funded"},
	{"Off-cycle payroll", domainFinance, "a correction, final pay or bonus must be paid outside the regular run"},
	{"Benefits carrier file feed", domainPeople, "the nightly enrollment file to a carrier fails or is rejected"},
	{"Timeclock outage", domainPeople, "badge timeclocks stop accepting punches on one or more units"},
	{"Badge access system outage", domainWorkplace, "door readers deny valid badges or the access panel goes offline"},
	{"EHR downtime", domainClinical, "the electronic health record is unavailable for planned or unplanned downtime"},
	{"SSO lockout recovery", domainTech, "employees cannot sign in through single sign-on"},
	{"New hire account provisioning", domainTech, "accounts for a start-date cohort must be ready before day one"},
	{"Terminated employee access removal", domainTech, "access must be removed at separation"},
	{"Scheduling system failover", domainTech, "the staff scheduling system is degraded or unavailable"},
	{"Paging and on-call escalation", domainTech, "a page is not acknowledged within the response window"},
	{"Database restore drill", domainTech, "the quarterly restore drill is due or a restore is requested"},
	{"Quarterly access review", domainTech, "the quarterly review of privileged and HR system access begins"},
	{"Leave of absence case handling", domainPeople, "an employee requests a leave of absence"},
	{"Workers' compensation claim intake", domainPeople, "an employee reports a work-related injury"},
	{"Wage garnishment processing", domainFinance, "a garnishment order or release arrives"},
	{"W-2 reissue", domainFinance, "an employee reports a missing or incorrect W-2"},
	{"Severance payment processing", domainFinance, "a separation agreement with severance is signed"},
	{"Final paycheck", domainPeople, "an employee separates and final pay deadlines apply"},
	{"Retro pay adjustment", domainFinance, "a pay change is approved with a past effective date"},
	{"Emergency staffing call-out", domainClinical, "a unit falls below its minimum staffing grid"},
	{"System outage communication", domainTech, "an outage affects employees and needs coordinated updates"},
	{"Employee data export request", domainLegal, "an employee asks for a copy of their personnel data"},
	{"Laptop loss or theft", domainTech, "a company laptop is lost or stolen"},
	{"Phishing report triage", domainTech, "an employee reports a suspicious email"},
	{"Vendor offboarding", domainFinance, "a vendor contract ends and access and payments must stop"},
	{"Benefits eligibility audit", domainPeople, "the annual dependent and eligibility audit begins"},
	{"Background check escalation", domainPeople, "a background check is delayed or returns a finding"},
	{"Relocation payment processing", domainFinance, "a relocation lump sum or reimbursement is approved"},
	{"Open enrollment go-live", domainPeople, "the open enrollment window opens"},
}

var seedMeetingSeries = []struct {
	Name, Domain string
	Count        int
	Agenda       []string
}{
	{"People Ops weekly sync", domainPeople, 10, []string{"Open requisitions and offer status", "Leave cases and accommodations", "Payroll exceptions from the last run", "Open enrollment readiness", "Manager training attendance", "Employee relations cases (no names)", "Policy updates in review"}},
	{"Comp committee", domainPeople, 5, []string{"Merit budget by division", "Pay band refresh proposals", "Promotion nominations above band", "Pay equity findings", "Retention bonus requests"}},
	{"HR leadership staff meeting", domainPeople, 6, []string{"Headcount against plan", "Turnover and exit themes", "Engagement survey follow-ups", "HR systems roadmap", "Budget and vendor renewals"}},
	{"Clinical staffing huddle", domainClinical, 6, []string{"Vacancy rate by unit", "Travel nurse contracts ending", "Float pool utilization", "Overtime hours trend", "New graduate cohort readiness"}},
	{"Engineering and People Ops sync", domainTech, 4, []string{"HRIS integration backlog", "Access provisioning delays", "Workspace feature requests", "Data retention changes"}},
	{"Open enrollment working group", domainPeople, 5, []string{"Carrier rate changes", "Communication plan and timeline", "Benefits fair logistics", "Decision support tool testing", "Employee questions log"}},
	{"Manager roundtable", domainPeople, 4, []string{"Giving feedback that lands", "Running calibration conversations", "Supporting employees on leave", "Hybrid team rituals"}},
	{"Quarterly business review", domainExec, 3, []string{"Revenue and margin", "Patient volume and quality measures", "Workforce metrics", "Top risks and mitigations"}},
}

var seedFAQs = []struct{ Topic, Domain string }{
	{"2026 open enrollment", domainPeople}, {"Medical plan comparison", domainPeople}, {"HSA and FSA", domainPeople},
	{"Dental and vision", domainPeople}, {"Employee assistance program", domainPeople}, {"401(k) match and vesting", domainFinance},
	{"Commuter benefits", domainPeople}, {"Life and disability insurance", domainPeople}, {"Leave of absence", domainPeople},
	{"Parental leave", domainPeople}, {"Tuition reimbursement", domainPeople}, {"Payroll and direct deposit", domainFinance},
	{"Pay transparency", domainLegal}, {"Remote work stipend", domainPeople}, {"Wellness program", domainPeople},
	{"Backup child care", domainPeople}, {"Benefits for part-time staff", domainPeople}, {"COBRA continuation", domainPeople},
	{"Qualifying life events", domainPeople}, {"Shift differentials", domainFinance},
}

var seedUnits = []string{"Med-surg 4 West", "Med-surg 5 East", "ICU", "Emergency department", "Telemetry", "Care coordination", "Night float pool", "Weekend option staff"}

var seedIncidents = []struct{ Name, Domain, Cause, Impact string }{
	{"Missed night differential on payroll run", domainFinance, "a pay code mapping was dropped when the timekeeping vendor renamed the night shift code", "214 night-shift employees were underpaid by an average of $61"},
	{"Duplicate direct deposits in off-cycle run", domainFinance, "the off-cycle batch was submitted twice after a timeout on the bank portal", "38 employees received a duplicate deposit that had to be reversed"},
	{"Badge readers offline in the east tower", domainWorkplace, "a failed network switch isolated the access control panels on floors 3 to 6", "staff used manual key access for 3 hours and 20 minutes"},
	{"Scheduling double-booking on ICU", domainClinical, "a swap approval and an open-shift pickup were accepted for the same slot", "one night shift was over-staffed and another under-staffed by one nurse"},
	{"Benefits enrollment window closed early", domainPeople, "the enrollment end date was set in UTC rather than local time", "evening-shift employees on the west coast lost the final five hours of the window"},
	{"New hire accounts provisioned late", domainTech, "the HRIS start-date feed skipped records with a pending background check flag", "11 new hires started without email or badge access"},
	{"Patient portal login outage", domainTech, "an expired signing certificate broke token validation", "patients could not sign in for 47 minutes"},
	{"Timeclock rounding error", domainPeople, "a configuration change applied 15-minute rounding instead of 7-minute rounding", "time for 96 employees was rounded incorrectly for one pay period"},
	{"Wrong PTO accrual rate for part-time staff", domainPeople, "the accrual plan used full-time hours for 0.6 FTE employees", "part-time staff over-accrued an average of 9.2 hours"},
	{"Offer letter sent with the wrong salary band", domainPeople, "the offer template pulled the 2025 band table", "two candidates received offers below the current minimum"},
	{"Pager escalation did not fire", domainTech, "an on-call schedule override had no end date and routed pages to an empty rotation", "a database alert went unacknowledged for 38 minutes"},
	{"Terminated contractor retained VPN access", domainTech, "contractor terminations were not part of the automated deprovisioning feed", "one account remained active for 12 days with no recorded use"},
	{"Payroll file rejected by the bank", domainFinance, "a new file format version was enabled without bank certification", "funding was delayed by four hours; deposits still landed on payday"},
	{"Open enrollment email sent to former employees", domainPeople, "the distribution list was built from a stale directory export", "412 former employees received the enrollment announcement"},
	{"Background check vendor delay", domainPeople, "a county court records backlog stalled checks for one region", "nine start dates moved by one week"},
	{"Staffing ratio breach on telemetry", domainClinical, "two call-outs arrived after the float pool had been assigned", "the unit ran at one nurse to six patients for four hours"},
	{"Laptop shipment lost in transit", domainTech, "the carrier misrouted a pallet at a regional hub", "six remote new hires started with loaner devices"},
	{"Workspace search outage", domainTech, "a reindex job exhausted database connections", "search returned no results for 25 minutes"},
	{"Shared drive permissions leak", domainTech, "a folder of interview notes was shared with the whole company by default", "the folder was visible to all staff for two days; no downloads were recorded"},
	{"Holiday pay miscalculation", domainFinance, "the holiday calendar omitted the observed date for a weekend holiday", "147 employees were paid regular rate for holiday hours"},
}

var seedCompDocs = []struct {
	Title string
	Table bool
}{
	{"2026 merit cycle guide for managers", true}, {"Merit budget allocation 2026", false}, {"Comp cycle calendar 2026", false},
	{"Promotion nomination guidelines", false}, {"Calibration session guide", false}, {"Pay band refresh 2026", false},
	{"Market adjustment request process", false}, {"Bonus eligibility rules 2026", false}, {"Pay equity analysis summary 2026", false},
	{"Manager talking points: merit letters", false}, {"Comp cycle FAQ for employees", false}, {"Off-cycle increase request process", false},
	{"Clinical ladder advancement criteria", false}, {"Sign-on and retention bonus guidelines", false}, {"Geographic pay differentials", false},
	{"Comp cycle retrospective 2025", false}, {"Job leveling framework", false}, {"Calibration agenda: clinical operations", false},
	{"Calibration agenda: engineering and product", false}, {"Calibration agenda: go-to-market", false},
}

var seedGuides = []struct{ Title, Domain string }{
	{"Manager guide to 1:1s", domainPeople}, {"Performance review guide 2026", domainPeople}, {"Goal-setting guide", domainPeople},
	{"Writing a structured job description", domainPeople}, {"Interview guide: registered nurse", domainClinical}, {"Interview guide: software engineer", domainTech},
	{"Interview guide: care coordinator", domainClinical}, {"Interview guide: account executive", domainGTM}, {"Interview guide: people partner", domainPeople},
	{"Interview guide: data analyst", domainTech}, {"Structured interview scorecard", domainPeople}, {"Exit interview guide", domainPeople},
	{"Offboarding checklist", domainPeople}, {"Recognition program guide", domainPeople}, {"Employee handbook 2026", domainPeople},
	{"How to request time off", domainPeople}, {"How to approve timesheets", domainPeople}, {"How to submit an expense report", domainFinance},
	{"How to update your direct deposit", domainFinance}, {"Hiring manager playbook", domainPeople}, {"Internal mobility guide", domainPeople},
	{"Mentorship program guide", domainPeople}, {"Accessibility at work guide", domainPeople}, {"Inclusive language guide", domainGTM},
	{"Stay interview guide", domainPeople},
}

var seedClinicalDocs = []string{
	"Nurse staffing ratio guideline: ICU", "Nurse staffing ratio guideline: telemetry", "Nurse staffing ratio guideline: med-surg",
	"Nurse staffing ratio guideline: emergency department", "Hand hygiene audit: Q1 2026", "Hand hygiene audit: Q2 2026", "Hand hygiene audit: Q3 2026",
	"Fall prevention bundle", "Medication reconciliation checklist", "Sepsis screening workflow", "Discharge planning checklist",
	"Care transitions playbook", "Patient complaint handling procedure", "Clinical escalation pathway", "Preceptor program guide",
	"Competency validation schedule 2026", "Charge nurse handbook", "Float pool orientation guide", "Restraint documentation standard",
	"Infection control daily checklist", "Safety huddle template", "Rapid response team guide", "Patient safety event reporting guide",
	"Clinical documentation audit results",
}

var seedTechDocs = []struct {
	Title string
	Table bool
}{
	{"On-call handbook", true}, {"Release checklist", false}, {"Incident severity levels", false},
	{"Architecture decision: event outbox for HR integrations", false}, {"Architecture decision: single sign-on provider", false},
	{"Architecture decision: document storage isolation", false}, {"Architecture decision: scheduling data model", false},
	{"Architecture decision: payroll export format", false}, {"Access request process", false}, {"Laptop standard build", false},
	{"Password and MFA standard", false}, {"Quarterly access review results", false}, {"Data retention for HR systems", false},
	{"People analytics dashboard definitions", false}, {"Headcount report definitions", false}, {"Turnover metric methodology", false},
	{"Workspace roadmap: second half 2026", false}, {"Payroll vendor API integration guide", false}, {"HRIS data dictionary", false},
	{"SQL style guide", false}, {"Postmortem template", false}, {"Service ownership map", false},
	{"Vendor security review checklist", false}, {"Security awareness training plan 2026", false}, {"Backup and restore standard", false},
	{"Change management process", false},
}

var seedOpsDocs = []struct {
	Title, Domain string
	Table         bool
}{
	{"Expense reimbursement rates 2026", domainFinance, false}, {"Budget planning calendar FY2027", domainFinance, false},
	{"Headcount planning guide", domainFinance, false}, {"Vendor payment terms", domainFinance, false},
	{"Record retention schedule", domainLegal, true}, {"I-9 verification procedure", domainLegal, false},
	{"Annual compliance training plan 2026", domainLegal, false}, {"Pay transparency law summary by state", domainLegal, false},
	{"Workplace posting requirements", domainLegal, false}, {"Litigation hold procedure", domainLegal, false},
	{"Office moves and seating guide", domainWorkplace, false}, {"Parking and transit guide", domainWorkplace, false},
	{"Visitor badge procedure", domainWorkplace, false}, {"Emergency evacuation plan: Harbor Street office", domainWorkplace, false},
	{"Ergonomics request process", domainWorkplace, false}, {"Customer onboarding playbook", domainGTM, false},
	{"Implementation kickoff checklist", domainGTM, false}, {"Sales compensation plan 2026", domainGTM, false},
	{"Customer escalation procedure", domainGTM, false}, {"Brand voice guide", domainGTM, false},
	{"Event planning checklist", domainGTM, false}, {"Content calendar: Q3 2026", domainGTM, false},
	{"Board meeting preparation checklist", domainExec, false}, {"Company all-hands agenda: June 2026", domainExec, false},
	{"Company all-hands agenda: September 2026", domainExec, false},
}

var seedShiftSwapExtras = []string{"Shift swap approval matrix", "Open shift pickup guidelines", "Holiday shift bidding process 2026", "Self-scheduling rules for nursing"}

// seedCatalog lists every document the seed creates, before owners, ages and
// audiences are chosen.
func seedCatalog() []seedSpec {
	var out []seedSpec
	for _, p := range seedPolicies {
		out = append(out, seedSpec{Kind: kindPolicy, Domain: p.Domain, Title: p.Name + " policy", Topic: p.Name, Table: p.Name == "Holiday pay"})
	}
	for _, role := range seedOnboardingRoles {
		out = append(out, seedSpec{Kind: kindOnboarding, Domain: domainPeople, Title: role + " onboarding checklist", Topic: role})
	}
	for _, title := range seedOnboardingGuides {
		out = append(out, seedSpec{Kind: kindOnboarding, Domain: domainPeople, Title: title, Topic: title})
	}
	for _, r := range seedRunbooks {
		out = append(out, seedSpec{Kind: kindRunbook, Domain: r.Domain, Title: r.System + " runbook", Topic: r.System})
	}
	for _, series := range seedMeetingSeries {
		for i := 0; i < series.Count; i++ {
			out = append(out, seedSpec{Kind: kindNotes, Domain: series.Domain, Title: series.Name, Topic: series.Name, Dated: true})
		}
	}
	for _, f := range seedFAQs {
		out = append(out, seedSpec{Kind: kindFAQ, Domain: f.Domain, Title: f.Topic + " FAQ", Topic: f.Topic})
	}
	for _, unit := range seedUnits {
		out = append(out, seedSpec{Kind: kindShiftSwap, Domain: domainClinical, Title: "Shift swap procedure: " + unit, Topic: unit})
	}
	for _, title := range seedShiftSwapExtras {
		out = append(out, seedSpec{Kind: kindShiftSwap, Domain: domainClinical, Title: title, Topic: title})
	}
	for _, inc := range seedIncidents {
		out = append(out, seedSpec{Kind: kindIncident, Domain: inc.Domain, Title: "Incident review: " + inc.Name, Topic: inc.Name})
	}
	for _, c := range seedCompDocs {
		out = append(out, seedSpec{Kind: kindComp, Domain: domainPeople, Title: c.Title, Topic: c.Title, Table: c.Table})
	}
	for _, g := range seedGuides {
		out = append(out, seedSpec{Kind: kindGuide, Domain: g.Domain, Title: g.Title, Topic: g.Title})
	}
	for _, title := range seedClinicalDocs {
		out = append(out, seedSpec{Kind: kindClinical, Domain: domainClinical, Title: title, Topic: title})
	}
	for _, d := range seedTechDocs {
		out = append(out, seedSpec{Kind: kindEngineer, Domain: domainTech, Title: d.Title, Topic: d.Title, Table: d.Table})
	}
	for _, d := range seedOpsDocs {
		out = append(out, seedSpec{Kind: kindOps, Domain: d.Domain, Title: d.Title, Topic: d.Title, Table: d.Table})
	}
	return out
}

// seedLink is a resolved link to an earlier seeded document.
type seedLink struct{ Title, ID string }

// seedBodyInput is everything one body needs.
type seedBodyInput struct {
	Spec         seedSpec
	Title        string
	Owner        seedPerson
	People       []seedPerson
	Links        []seedLink
	Written      time.Time
	Rand         *rand.Rand
	RevisionNote string
	IsRevision   bool
}

func pick[T any](r *rand.Rand, items []T) T { return items[r.IntN(len(items))] }

func pickN[T any](r *rand.Rand, items []T, n int) []T {
	if n > len(items) {
		n = len(items)
	}
	idx := r.Perm(len(items))[:n]
	out := make([]T, 0, n)
	for _, i := range idx {
		out = append(out, items[i])
	}
	return out
}

func bullets(lines []string) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("- " + line + "\n")
	}
	return b.String()
}

func numbered(lines []string) string {
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%d. %s\n", i+1, line)
	}
	return b.String()
}

func relatedSection(links []seedLink) string {
	if len(links) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Related documents\n\n")
	for _, l := range links {
		fmt.Fprintf(&b, "- [%s](doc:%s)\n", l.Title, l.ID)
	}
	return b.String()
}

func personName(p seedPerson) string {
	if p.Name != "" {
		return p.Name
	}
	return p.Key
}

func firstName(p seedPerson) string {
	name := personName(p)
	if i := strings.IndexByte(name, ' '); i > 0 {
		return name[:i]
	}
	return name
}

func ownerLine(in seedBodyInput) string {
	title := in.Owner.Title
	if title == "" {
		title = "HarborCare"
	}
	return fmt.Sprintf("**Owner:** %s, %s · **Last reviewed:** %s\n", personName(in.Owner), title, in.Written.Format("January 2, 2006"))
}

// documentSeedBody renders one document's Markdown.
func documentSeedBody(in seedBodyInput) string {
	var body string
	switch in.Spec.Kind {
	case kindPolicy:
		body = policyBody(in)
	case kindOnboarding:
		body = onboardingBody(in)
	case kindRunbook:
		body = runbookBody(in)
	case kindNotes:
		body = notesBody(in)
	case kindFAQ:
		body = faqBody(in)
	case kindShiftSwap:
		body = shiftSwapBody(in)
	case kindIncident:
		body = incidentBody(in)
	case kindComp:
		body = compBody(in)
	case kindClinical:
		body = clinicalBody(in)
	default:
		body = generalBody(in)
	}
	if in.IsRevision && in.RevisionNote != "" {
		body += "\n> **Draft note:** " + in.RevisionNote + "\n"
	}
	return body + relatedSection(in.Links)
}

func findPolicy(name string) policyTopic {
	for _, p := range seedPolicies {
		if p.Name == name {
			return p
		}
	}
	return policyTopic{Name: name, Purpose: "Sets HarborCare's expectations for " + strings.ToLower(name) + "."}
}

var policyScopes = []string{
	"This policy applies to all HarborCare employees, including part-time and per-diem staff, in every state where we operate.",
	"This policy applies to all employees and to contractors working on HarborCare premises or systems.",
	"This policy applies to all employees. Where a collective bargaining agreement or state law provides more, that provision governs.",
}

var policyProcedures = [][]string{
	{"Review the policy and the related FAQ before submitting a request.", "Submit the request in the workspace under **Time and absence** or **My requests**.", "Your manager reviews it and responds within three business days.", "People Operations confirms anything that affects pay before the next payroll cutoff."},
	{"Talk to your manager first; most questions are resolved there.", "If you need an exception, open a People Operations case with the details.", "A People Partner reviews the case and may ask for supporting information.", "The decision and its reasoning are recorded on the case."},
	{"Managers check eligibility in the workspace before approving.", "Approved items flow to payroll automatically at the next cutoff.", "Employees can see the status of every request in **My requests**.", "Disputes go to the People Partner for the business unit."},
}

func policyBody(in seedBodyInput) string {
	p := findPolicy(in.Spec.Topic)
	r := in.Rand
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n## Purpose\n\n%s\n\n## Scope\n\n%s\n\n## Policy\n\n", in.Title, ownerLine(in), p.Purpose, pick(r, policyScopes))
	b.WriteString(bullets(p.Rules))
	if in.Spec.Table {
		b.WriteString("\n### Observed holidays 2026\n\n| Holiday | Date observed | Premium for hours worked |\n| --- | --- | --- |\n")
		for _, row := range [][3]string{{"New Year's Day", "January 1", "1.5x"}, {"Martin Luther King Jr. Day", "January 19", "1.5x"}, {"Memorial Day", "May 25", "1.5x"}, {"Juneteenth", "June 19", "1.5x"}, {"Independence Day", "July 3", "1.5x"}, {"Labor Day", "September 7", "1.5x"}, {"Thanksgiving Day", "November 26", "1.5x"}, {"Christmas Day", "December 25", "2x"}} {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", row[0], row[1], row[2])
		}
	}
	b.WriteString("\n## Procedure\n\n")
	b.WriteString(numbered(pick(r, policyProcedures)))
	b.WriteString("\n## Exceptions\n\n")
	b.WriteString(pick(r, []string{
		"Exceptions require written approval from the Director of People Operations and are reviewed each year.",
		"Where local law is more generous, the local requirement applies. Other exceptions are approved by People Operations.",
		"No exceptions are made to legal minimums. Other exceptions need a People Partner's written approval.",
	}) + "\n\n## Questions\n\nContact your People Partner or post in the **#ask-people-ops** channel.\n")
	return b.String()
}

var onboardingDayOne = []string{"Collect your badge at the security desk in the Harbor Street lobby", "Complete I-9 document review with People Operations", "Sign in to the workspace and set up multi-factor authentication", "Meet your manager and onboarding buddy", "Review the code of conduct and sign the acknowledgment", "Pick up your laptop and confirm VPN access", "Take the building tour and find your team's area"}
var onboardingWeekOne = []string{"Finish required compliance training (HIPAA, harassment prevention, safety)", "Set up direct deposit and tax withholding", "Enroll in benefits; the 30-day window starts on your hire date", "Shadow a teammate for at least two shifts or working sessions", "Agree on 30-60-90 day goals with your manager", "Join the team channels and the weekly sync", "Book introductions with key partners"}
var onboardingRoleTasks = map[string][]string{
	"Registered nurse":          {"Complete EHR training and pass the documentation check", "Validate unit competencies with your preceptor", "Complete medication administration and pump training"},
	"Nurse practitioner":        {"Confirm credentialing and privileges are approved", "Set up e-prescribing and DEA registration", "Meet the collaborating physician"},
	"Care coordinator":          {"Shadow discharge planning rounds", "Learn the referral and prior-authorization workflow", "Review the care transitions playbook"},
	"Software engineer":         {"Clone the main repository and run the test suite", "Ship a small change to production with your buddy", "Read the architecture decisions for your team's services"},
	"Site reliability engineer": {"Get access to monitoring and paging", "Shadow one on-call week before joining the rotation", "Walk through the database restore drill"},
	"Data analyst":              {"Request access to the analytics warehouse", "Review metric definitions and the data dictionary", "Rebuild one existing dashboard to learn the model"},
	"Product manager":           {"Read the current roadmap and last quarter's review", "Sit in on five customer or employee interviews", "Meet the design and engineering leads for your area"},
	"Account executive":         {"Complete product certification", "Shadow three discovery calls", "Get CRM access and review your territory plan"},
	"Customer success manager":  {"Review your book of accounts and health scores", "Join two implementation kickoffs", "Learn the escalation procedure"},
	"People partner":            {"Meet the leaders of your client groups", "Review open employee relations cases with your manager", "Learn the leave and accommodation workflow"},
	"Accountant":                {"Get access to the general ledger and close checklist", "Shadow the month-end close", "Review the vendor payment terms"},
	"Security engineer":         {"Review the incident response plan", "Get access to the security tooling and alert queue", "Read the vendor security review checklist"},
	"IT systems administrator":  {"Get admin access through the privileged access request", "Review the laptop standard build", "Shadow new hire provisioning"},
	"Workplace coordinator":     {"Learn the visitor badge procedure", "Walk the evacuation routes for each floor", "Meet the building management contacts"},
	"Patient safety specialist": {"Review the last quarter's safety events", "Shadow a safety huddle on two units", "Learn the event reporting system"},
}

func onboardingBody(in seedBodyInput) string {
	r := in.Rand
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n", in.Title, ownerLine(in))
	if tasks, ok := onboardingRoleTasks[in.Spec.Topic]; ok {
		fmt.Fprintf(&b, "Welcome to HarborCare. This checklist covers everything a new %s needs in the first month. Your manager and your onboarding buddy will check in at the end of each week.\n\n", strings.ToLower(in.Spec.Topic))
		b.WriteString("## Day one\n\n" + bullets(pickN(r, onboardingDayOne, 5)))
		b.WriteString("\n## First week\n\n" + bullets(pickN(r, onboardingWeekOne, 5)))
		b.WriteString("\n## Role-specific\n\n" + bullets(tasks))
		b.WriteString("\n## By day 30\n\n" + bullets([]string{"Review your 30-day goals with your manager", "Complete the onboarding survey", "Tell us what was confusing so we can fix it for the next person"}))
		return b.String()
	}
	fmt.Fprintf(&b, "This guide is maintained by People Operations and updated after every onboarding cohort.\n\n## Overview\n\n%s\n\n## Timeline\n\n", pick(r, []string{
		"New hires start on Mondays so orientation can run as a cohort. Everything below is scheduled in the workspace before the start date.",
		"The goal is a first week where nobody waits for access, equipment or information.",
		"Managers own the experience; People Operations owns the logistics.",
	}))
	b.WriteString(numbered([]string{"Two weeks before: offer accepted, background check started, equipment ordered", "One week before: accounts provisioned, buddy assigned, welcome email sent", "Day one: orientation, badge, laptop, benefits overview", "Week one: training plan, team introductions, first goals", "Day 30: check-in with the manager and the onboarding survey"}))
	b.WriteString("\n## Who does what\n\n" + bullets([]string{"**Hiring manager:** schedule, first-week plan, buddy assignment", "**People Operations:** paperwork, I-9, benefits enrollment", "**IT:** accounts, laptop, access requests", "**Workplace services:** badge, desk and parking"}))
	return b.String()
}

func runbookBody(in seedBodyInput) string {
	r := in.Rand
	var symptom string
	for _, rb := range seedRunbooks {
		if rb.System == in.Spec.Topic {
			symptom = rb.Symptom
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n## When to use this runbook\n\nUse this runbook when %s. Follow the steps in order and record what you did in the case or incident channel.\n\n", in.Title, ownerLine(in), symptom)
	b.WriteString("## Before you start\n\n" + bullets(pickN(r, []string{"Confirm you have the admin role for the affected system", "Open a case so every step has a timestamp", "Check the change calendar for anything deployed in the last 24 hours", "Tell the on-call lead you are working the issue", "Check whether the payroll or scheduling cutoff is within the next four hours"}, 3)))
	b.WriteString("\n## Steps\n\n")
	steps := []string{
		"Confirm the scope: which sites, units or employee groups are affected, and since when.",
		"Check the integration dashboard for failed jobs and note the error codes.",
		"If the vendor is involved, open a priority ticket and record the ticket number.",
		"Apply the documented workaround and confirm with one affected person that it worked.",
		"Re-run the failed job or process for the affected records only.",
		"Reconcile the counts: records expected, processed and failed.",
		"Post a status update in the incident channel every 30 minutes until resolved.",
		"Close the case with a short summary and link any follow-up tickets.",
	}
	b.WriteString(numbered(steps[:4+r.IntN(4)]))
	b.WriteString("\n## Escalation\n\n")
	fmt.Fprintf(&b, "If the issue is not contained within one hour, page the secondary on-call and notify %s. For anything that affects pay or patient care, also notify the Director of People Operations or the house supervisor.\n", personName(pick(r, in.People)))
	b.WriteString("\n## Verification\n\n" + bullets([]string{"Affected users confirm the fix", "No new errors in the last two job runs", "Case updated with the final counts"}))
	return b.String()
}

var notesDecisions = []string{
	"Move the policy review deadline to the end of the month.", "Pilot the new intake form with two units before rolling out.", "Hold the merit budget at 3.5 percent pending the finance review.",
	"Send the FAQ update to managers before it goes company-wide.", "Add a second approver for off-cycle payments over $10,000.", "Publish the open enrollment calendar next week.",
	"Retire the old leave request form on the first of next month.", "Ask the vendor for a root cause before renewing.", "Keep weekly check-ins through the end of the quarter.",
}

var notesDiscussion = []string{
	"Time-to-fill is down to 41 days, but nursing roles are still above 60.", "Managers asked for a simpler way to see who is on leave next week.",
	"The last payroll run had three exceptions, all related to retro pay.", "Several employees asked whether the tuition benefit covers certificates; it does if the program is accredited.",
	"Survey participation reached 78 percent; comments on workload are concentrated in two units.", "The carrier rate increase is 6.8 percent, below the 8 percent we budgeted.",
	"We need a clearer handoff between recruiting and onboarding for clinical hires.", "Night-shift staff find it hard to attend daytime training; we will record sessions.",
	"The accommodation backlog is down to four open cases.", "Two managers volunteered to pilot the new calibration template.",
}

func notesBody(in seedBodyInput) string {
	r := in.Rand
	var agenda []string
	for _, s := range seedMeetingSeries {
		if s.Name == in.Spec.Topic {
			agenda = s.Agenda
		}
	}
	attendees := pickN(r, in.People, 4+r.IntN(4))
	names := make([]string, 0, len(attendees)+1)
	names = append(names, personName(in.Owner))
	for _, p := range attendees {
		if p.Key != in.Owner.Key {
			names = append(names, personName(p))
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n**Date:** %s · **Facilitator:** %s\n\n**Attendees:** %s\n\n## Agenda\n\n", in.Title, in.Written.Format("Monday, January 2, 2006"), personName(in.Owner), strings.Join(names, ", "))
	items := pickN(r, agenda, 3)
	b.WriteString(numbered(items))
	b.WriteString("\n## Notes\n\n")
	for _, item := range items {
		fmt.Fprintf(&b, "### %s\n\n%s\n\n", item, pick(r, notesDiscussion))
	}
	b.WriteString("## Decisions\n\n" + bullets(pickN(r, notesDecisions, 2)))
	b.WriteString("\n## Action items\n\n")
	for i, action := range pickN(r, []string{"Draft the updated FAQ", "Share the numbers with finance", "Book the vendor follow-up", "Update the manager guide", "Send the summary to the leadership channel", "Collect feedback from the pilot units", "Close out the open cases"}, 3) {
		owner := attendees[i%len(attendees)]
		fmt.Fprintf(&b, "- **%s:** %s by %s\n", firstName(owner), action, in.Written.AddDate(0, 0, 7).Format("Jan 2"))
	}
	return b.String()
}

func faqBody(in seedBodyInput) string {
	r := in.Rand
	topic := in.Spec.Topic
	lower := strings.ToLower(topic)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\nAnswers to the questions People Operations hears most about %s. If your question is not here, ask in **#ask-people-ops**.\n\n", in.Title, ownerLine(in), lower)
	qas := [][2]string{
		{"Who is eligible?", "Regular full-time and part-time employees scheduled for 20 or more hours a week are eligible from their first day. Per-diem staff should check the part-time benefits FAQ."},
		{"When do changes take effect?", "Changes made during the enrollment window take effect on January 1. Changes after a qualifying life event take effect on the first of the following month."},
		{"Where do I make changes?", "Open the workspace, choose **Benefits**, then **Make a change**. You will see a summary to confirm before anything is submitted."},
		{"What if I miss the deadline?", "You keep your current elections, except for FSA contributions, which must be re-elected every year."},
		{"How do I add a dependent?", "Add the dependent in the workspace and upload a birth certificate, marriage certificate or court order within 30 days."},
		{"Who can I talk to?", "Book time with the benefits team through the workspace, or call the benefits line Monday to Friday, 8 a.m. to 6 p.m."},
		{"Does this affect my paycheck?", "Premiums and contributions come out of each paycheck before taxes where the law allows. Your first adjusted paycheck shows the change."},
		{"Is this taxable?", "Most benefits are pre-tax. Imputed income applies to some coverage, such as domestic partner coverage, and appears on your pay statement."},
	}
	for _, qa := range pickN(r, qas, 5) {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", qa[0], qa[1])
	}
	return b.String()
}

func shiftSwapBody(in seedBodyInput) string {
	r := in.Rand
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n", in.Title, ownerLine(in))
	fmt.Fprintf(&b, "Shift swaps let staff trade scheduled shifts without changing the unit's staffing grid. %s\n\n## Rules\n\n", pick(r, []string{
		"Swaps are allowed between staff with the same role and unit competencies.",
		"Every swap must keep the unit at or above its minimum skill mix.",
		"Swaps are approved in the scheduling system so payroll sees the right hours.",
	}))
	b.WriteString(bullets(pickN(r, []string{
		"Both people must agree in the scheduling system before the request reaches the charge nurse.",
		"Requests are submitted at least 48 hours before the earlier shift.",
		"A swap may not create more than 40 hours in a week without overtime approval.",
		"No more than four swaps per person per schedule period.",
		"New graduates in their first 90 days swap only with other orientees or with preceptor approval.",
		"Holiday shifts can be swapped only with another holiday shift.",
		"Charge nurse shifts swap only with another charge-qualified nurse.",
	}, 5)))
	b.WriteString("\n## How to request a swap\n\n")
	b.WriteString(numbered([]string{"Open the schedule and select the shift you want to give up.", "Choose **Offer swap** and pick the colleague and the shift you will take.", "Your colleague accepts the swap in their schedule.", "The charge nurse or unit manager approves or declines within 24 hours.", "Both schedules update and you both get a confirmation."}))
	b.WriteString("\n## Approvals\n\n" + pick(r, []string{
		"The unit manager approves swaps during business hours; the house supervisor approves after hours and on weekends.",
		"The charge nurse approves same-week swaps; the unit manager approves anything further out.",
	}) + "\n")
	return b.String()
}

func incidentBody(in seedBodyInput) string {
	r := in.Rand
	cause, impact := "a configuration change", "a small number of employees were affected"
	for _, inc := range seedIncidents {
		if inc.Name == in.Spec.Topic {
			cause, impact = inc.Cause, inc.Impact
		}
	}
	start := in.Written.AddDate(0, 0, -3-r.IntN(5))
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n**Status:** Closed · **Severity:** %s · **Review lead:** %s\n\n", in.Title, pick(r, []string{"SEV-2", "SEV-3", "SEV-3"}), personName(in.Owner))
	fmt.Fprintf(&b, "## Summary\n\nOn %s, %s. The root cause was %s. This review is blameless: it describes what happened and what we will change.\n\n", start.Format("January 2"), impact, cause)
	b.WriteString("## Timeline\n\n")
	b.WriteString(bullets([]string{
		start.Format("Jan 2") + " 06:40 — First report in the service desk queue",
		start.Format("Jan 2") + " 07:15 — On-call confirms the issue and opens the incident channel",
		start.Format("Jan 2") + " 08:05 — Cause identified and workaround applied",
		start.Format("Jan 2") + " 11:30 — Correction completed and verified with affected staff",
		start.AddDate(0, 0, 1).Format("Jan 2") + " 16:00 — Communication sent to affected employees",
	}))
	fmt.Fprintf(&b, "\n## Impact\n\n%s. No patient care was affected.\n\n## Root cause\n\nThe direct cause was %s. %s\n\n", strings.ToUpper(impact[:1])+impact[1:], cause, pick(r, []string{
		"The change was reviewed, but the review checklist did not ask about downstream payroll or scheduling effects.",
		"Monitoring existed but alerted on job failure, not on unexpected output, so the problem surfaced through employee reports.",
		"The process depended on one person's knowledge and was not written down.",
	}))
	b.WriteString("## What went well\n\n" + bullets(pickN(r, []string{"Employees reported the problem quickly through the service desk", "The workaround was documented and applied within an hour", "Finance and People Operations agreed on corrections the same day", "Communication to affected staff was clear and went out within a day"}, 2)))
	b.WriteString("\n## Action items\n\n")
	for i, action := range pickN(r, []string{"Add an output check to the nightly job", "Update the change checklist with a payroll impact question", "Write the missing runbook step", "Add an alert for unexpected record counts", "Run a tabletop exercise with the vendor", "Review access for everyone who can change the configuration"}, 3) {
		fmt.Fprintf(&b, "- %s (owner: %s, due %s)\n", action, firstName(in.People[(i*7+r.IntN(len(in.People)))%len(in.People)]), in.Written.AddDate(0, 0, 14+7*i).Format("Jan 2"))
	}
	return b.String()
}

func compBody(in seedBodyInput) string {
	r := in.Rand
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n", in.Title, ownerLine(in))
	b.WriteString(pick(r, []string{
		"This guide explains how the 2026 compensation cycle works and what managers need to do at each step. Numbers are confidential until letters are delivered.",
		"The compensation cycle rewards performance and keeps pay competitive and fair. This document is for people managers and People Partners.",
		"Use this document together with the merit guide and the comp cycle calendar.",
	}) + "\n\n## Key dates\n\n")
	b.WriteString(bullets([]string{"**Budgets released:** February 2", "**Manager recommendations due:** February 20", "**Calibration sessions:** February 23 to March 6", "**Final approvals:** March 13", "**Letters delivered:** March 23 to March 27", "**Effective date:** April 1 pay period"}))
	if in.Spec.Table {
		b.WriteString("\n## Merit matrix\n\nRecommended increase by performance rating and position in range.\n\n| Rating | Below midpoint | Near midpoint | Above midpoint |\n| --- | --- | --- | --- |\n| Exceptional | 6.0% | 5.0% | 4.0% |\n| Strong | 4.5% | 3.5% | 2.5% |\n| Solid | 3.0% | 2.5% | 1.5% |\n| Developing | 1.0% | 0% | 0% |\n")
	}
	b.WriteString("\n## Guidance\n\n")
	b.WriteString(bullets(pickN(r, []string{
		"Recommendations above the matrix need a written rationale.",
		"Promotions are budgeted separately from merit and should land at least at the new band minimum.",
		"Check every recommendation against the pay equity flags in the planning tool.",
		"Employees on leave are reviewed on the same schedule; letters are delivered when they return.",
		"Do not discuss numbers with employees until the letters are released.",
		"Part-time employees receive the same percentage as full-time peers.",
		"Clinical ladder promotions follow the ladder criteria and take effect with the cycle.",
	}, 4)))
	b.WriteString("\n## Questions\n\nContact your People Partner or the compensation team at **#comp-cycle-2026**.\n")
	return b.String()
}

func clinicalBody(in seedBodyInput) string {
	r := in.Rand
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n", in.Title, ownerLine(in))
	if strings.HasPrefix(in.Title, "Hand hygiene audit") {
		fmt.Fprintf(&b, "## Results\n\nObserved compliance across all units was **%d percent**, against a target of 95 percent.\n\n", 86+r.IntN(10))
		b.WriteString(bullets([]string{fmt.Sprintf("ICU: %d%%", 88+r.IntN(11)), fmt.Sprintf("Telemetry: %d%%", 84+r.IntN(12)), fmt.Sprintf("Med-surg: %d%%", 85+r.IntN(12)), fmt.Sprintf("Emergency department: %d%%", 80+r.IntN(14))}))
		b.WriteString("\n## Observations\n\n" + bullets(pickN(r, []string{"Missed opportunities cluster at room exit, not entry.", "Dispensers near two ICU rooms were empty during observation.", "Night shift compliance is lower than day shift.", "Glove use is sometimes substituted for hand hygiene."}, 3)))
		b.WriteString("\n## Next steps\n\n" + numbered([]string{"Share unit results at the next safety huddle.", "Add dispenser checks to the environmental rounds.", "Repeat observations on night shift next month."}))
		return b.String()
	}
	b.WriteString("## Purpose\n\n" + pick(r, []string{
		"Standardizes practice across units so every patient gets the same safe care.",
		"Gives charge nurses and staff one reference for this workflow.",
		"Supports our quality and safety goals for 2026.",
	}) + "\n\n## Standard\n\n")
	b.WriteString(bullets(pickN(r, []string{
		"Follow the steps for every patient, every shift.",
		"Document completion in the EHR before the end of the shift.",
		"Escalate concerns to the charge nurse immediately.",
		"Use the rapid response team for any acute change in condition.",
		"Review the checklist at every handoff.",
		"New staff complete the competency before working independently.",
	}, 4)))
	b.WriteString("\n## Workflow\n\n" + numbered(pickN(r, []string{
		"Assess the patient using the unit's screening tool.",
		"Discuss findings at the safety huddle.",
		"Complete the checklist items and sign in the EHR.",
		"Communicate the plan to the patient and family.",
		"Hand off using the structured report format.",
		"Audit five charts a month and share results with the unit.",
	}, 5)))
	return b.String()
}

func generalBody(in seedBodyInput) string {
	r := in.Rand
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n", in.Title, ownerLine(in))
	if in.Spec.Table && strings.Contains(in.Title, "On-call") {
		b.WriteString("## Rotation\n\n| Week | Primary | Secondary |\n| --- | --- | --- |\n")
		for i := 0; i < 4; i++ {
			a, c := in.People[(i*3)%len(in.People)], in.People[(i*3+1)%len(in.People)]
			fmt.Fprintf(&b, "| %s | %s | %s |\n", in.Written.AddDate(0, 0, 7*i).Format("Jan 2"), personName(a), personName(c))
		}
		b.WriteString("\n")
	}
	if in.Spec.Table && strings.Contains(in.Title, "retention") {
		b.WriteString("## Schedule\n\n| Record | Retention | Owner |\n| --- | --- | --- |\n| Personnel file | 7 years after separation | People Operations |\n| I-9 forms | 3 years after hire or 1 year after separation, whichever is later | People Operations |\n| Payroll records | 7 years | Finance |\n| Medical and leave records | 7 years, stored separately | People Operations |\n| Interview notes | 3 years | Talent Acquisition |\n| Safety incident reports | 5 years | Quality and Safety |\n\n")
	}
	b.WriteString("## Overview\n\n" + pick(r, []string{
		"This document is the reference for the process described below. It is reviewed every six months or after any incident that involves it.",
		"We wrote this down because the same questions kept coming up. Suggest changes in the comments.",
		"This is the current standard. Earlier versions are in the history panel.",
	}) + "\n\n## Details\n\n")
	b.WriteString(bullets(pickN(r, []string{
		"Requests go through the workspace so there is one record of every decision.",
		"Anything involving employee personal data follows the data retention standard.",
		"Owners review this document every six months.",
		"Exceptions need approval from the document owner and are recorded here.",
		"Changes that affect pay are coordinated with Finance before they take effect.",
		"Keep customer and patient information out of chat; link to the system of record instead.",
		"Every step has a named owner and a backup.",
	}, 4)))
	b.WriteString("\n## Steps\n\n" + numbered(pickN(r, []string{
		"Read this document and the related runbook.",
		"Open a request in the workspace with the details.",
		"The owner reviews it within two business days.",
		"Approved changes are scheduled and announced.",
		"The requester confirms the result and closes the request.",
	}, 4)))
	fmt.Fprintf(&b, "\n## Contacts\n\n- **Owner:** %s\n- **Backup:** %s\n", personName(in.Owner), personName(pick(r, in.People)))
	return b.String()
}

var revisionNotes = []string{
	"Updating the eligibility section for the new state requirements; not published yet.",
	"Reworking the steps after the last incident review. Comments welcome before I publish.",
	"Adding the 2027 dates; waiting on finance to confirm.",
	"Tightening the wording based on legal's feedback.",
	"New section on part-time staff still in progress.",
}
