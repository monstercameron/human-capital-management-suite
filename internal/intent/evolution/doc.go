// Package evolution provides pure compatibility, digest and supersession
// primitives for intent-definition changes. These primitives do not govern
// production definitions or live instances by themselves: production currently
// constructs only BOOTSTRAP registries, has no caller of
// [intent.Registry.PublishManaged], and applies no supersession to live
// instances. The controls remain dormant until those runtime paths are
// connected.
//
// The policy primitives describe these successor cases:
//
//   - [CompatibilityCheck] classifies a successor version against its
//     predecessor. The only COMPATIBLE differences are optional-input
//     additions/removals, a required input becoming optional, a lower effect
//     class, and adding an approval requirement.
//   - Every named incompatible difference — a required input added (including
//     an existing input that became required), a required input removed, an
//     input's declared kind changed, the effect class raised, or the approval
//     requirement removed — is recorded in a report. [SupersessionRecord]
//     describes a possible governance artifact for an incompatible successor,
//     including a reason, distinct author and approver, an effective instant,
//     and a [LiveInstancePolicy]. This package constructs and validates the
//     artifact; it does not perform approval, publication, migration or
//     cutover.
//
// [DefinitionDigest] computes stable content identity, and
// [RefuseInPlaceEdit] detects an edit when a caller compares definitions with
// the same (intent_type_id, version). These helpers do not prevent such edits
// unless a publication path invokes them.
//
// This package makes no governance decision and touches no store. A caller
// with real authority must record and act on its reports and records. The
// live-instance compatibility question for intent definitions has the same
// shape as [internal/workflow/migrationpreview]'s workflow-version preview.
package evolution
