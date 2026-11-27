---
name: medical-slides
description: Build medical and pharmaceutical presentations — study readouts, product profiles, medical education — with evidence-bearing slides, correct tables, and compliance-aware wording, delivered as a verified .pptx.
---

# Medical and pharmaceutical slide decks

Use with `slides-design` (visual system) and `office-pptx` (python-pptx API and
file mechanics). This skill decides what goes on each slide for a clinical,
medical-affairs or pharma audience, and how claims are worded and sourced.

Drafting support only: a qualified medical/regulatory reviewer must approve the
final content. Never present the output as medical advice or as approved
promotional material.

## Before writing

Establish or infer, and state your assumption if the user is silent:

- Audience: clinicians, medical affairs, regulators, payers, investors, patients.
- Purpose: study readout, disease education, product profile, advisory board,
  congress deck, internal decision.
- Language (zh/en) and length; 10–15 slides is a typical working deck.
- Data cut / date, study identifiers, and whether data are interim.

Outline first (3–6 bullets), then build slides. Do not start from the template
gallery or from a wall of text.

## Evidence on every clinical claim

Each efficacy or safety statement needs, on the same slide or its footnote:

`study / phase · population and n · intervention · endpoint · timepoint ·
effect size with CI and p` — and whether it was pre-specified.

- Separate **primary** from secondary, post-hoc and exploratory results; label
  post-hoc explicitly.
- Observational data: use association language ("was associated with"), never
  "causes", "proves" or "prevents".
- Report absolute as well as relative effects where possible (ARR/NNT, not just
  a hazard ratio).
- Do not compare across trials as if they were head-to-head. If you show a
  cross-trial table, say "cross-trial comparison, not adjusted".
- Mark investigational, off-label or unapproved uses on the slide, not only in
  the speaker notes.
- Every table/chart carries a source line: registry ID, publication citation,
  or "data on file, internal analysis".

## Deck structures

**Clinical study readout** (12–16 slides)
Title → disclosures → background and unmet need → objectives → study design
(arms, randomisation, endpoints, analysis sets) → disposition (CONSORT-style
counts) → baseline demographics → primary endpoint → key secondary endpoints →
safety (TEAE/SAE tables) → PK/PD if relevant → limitations → conclusions →
references → appendix.

**Disease / landscape** (10–14)
Burden and epidemiology (incidence vs prevalence, geography, year) → current
pathway and gaps → mechanism → evidence landscape → positioning options →
access/reimbursement considerations → discussion questions.

**Medical education** (10–15)
Learning objectives → case or clinical scenario → evidence by question →
practice points → common pitfalls → references. Keep promotional tone out.

**Product profile** (10–14)
Approved indication wording → mechanism → pivotal efficacy → safety profile →
dosing/administration → special populations → formulary/access → references.

## Tables that hold up

- Safety by MedDRA SOC/PT: `n (%)` with the same denominator; show overall TEAE,
  treatment-related, grade ≥3, SAE, discontinuations, deaths separately.
- Efficacy: arm columns, endpoint rows, effect + 95% CI, p; put the analysis set
  (ITT/PP) in the header.
- Baseline: mean/median with SD/IQR as appropriate; do not mix.
- Units and significant figures consistent across slides; do not round an
  endpoint's CI so tightly that it implies more precision than the study had.
- Never fabricate a Kaplan–Meier curve or forest plot: plot only from supplied
  data, otherwise show a table.

## Wording and compliance

- Generic names lower-case (unless at sentence start), brand names capitalised;
  define abbreviations at first use on each slide or in a footnote.
- Avoid superlatives ("best-in-class", "safe", "cure") and causal claims.
- Distinguish statistically significant from clinically meaningful.
- No patient identifiers, faces, dates of birth or free-text that could
  re-identify a patient; de-identified case text only.
- Add a footer on every content slide: data cut, "for internal discussion" or
  the appropriate disclosure, and the draft date.
- Reference style: Vancouver with PMID/DOI where available; preprints labelled.

## QA specific to medical decks

- Numbers match the source table exactly; denominators match the analysis set.
- Every efficacy/safety claim has its source and timepoint visible.
- Primary vs secondary vs post-hoc is unambiguous.
- Indication wording matches the approved label; unapproved uses are labelled.
- Limitations slide present for any study readout; no conclusion outruns the
  data.
- Reopen the `.pptx`, check slide count, then deliver with the path and a short
  summary of what was verified and what still needs expert review.
