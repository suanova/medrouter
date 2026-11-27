---
name: medical-writing
description: Write and review medical, clinical and pharmaceutical prose — terminology, evidence language, statistics, citations, privacy and bilingual zh/en conventions.
---

# Medical and pharmaceutical writing

Use for study reports, manuscripts, medical-information answers, slide notes,
patient summaries and translations. Pair with `medical-slides` when the output
is a deck and `pharma-trials` when it concerns trial design or reporting.

Drafting support only: a qualified medical/regulatory reviewer signs off. Do not
phrase output as diagnosis, prescription or personal medical advice.

## Terminology

- Generic (INN) names in lower case: `pembrolizumab`; brand names as written:
  `Keytruda`. Give the generic name at first mention even when the request uses
  a brand.
- Define every abbreviation at first use in the document and again on each slide;
  keep one expansion per abbreviation (do not reuse "AE" for two concepts).
- Units: SI with conventional units in parentheses when the audience expects
  them (`mmol/L (mg/dL)`); keep a space between number and unit (`5 mg`,
  `37 °C`), except `%` and angles.
- Significant figures: match the source. A p-value of 0.04 is not 0.040; report
  `<0.001` rather than 0.000.
- Distinguish incidence (new cases per population per time) from prevalence
  (existing cases at a time point); attack rate, risk and rate are not
  interchangeable.

## Evidence language

| Situation | Use | Avoid |
|---|---|---|
| Randomised superiority | "reduced", "improved" with effect + CI + p | "proves", "cures" |
| Non-significant result | "did not significantly differ", report CI | "no effect", "equivalent" |
| Observational association | "was associated with" | "causes", "leads to" |
| Subgroup / post-hoc | "in a post-hoc subgroup analysis" | presenting as primary |
| Single-arm / open-label | "in this single-arm study" | comparative verbs |
| Preclinical / in vitro | "in vitro", "in animal models" | human implications without hedging |

- Never call a difference "significant" unless a statistical test supports it;
  say "clinically meaningful" only with a stated threshold.
- Report absolute effects alongside relative ones (risk difference, NNT/NNH).
- State missing data, analysis set (ITT/PP) and multiplicity handling when they
  affect interpretation.
- Hedge appropriately: "may", "was associated with", "consistent with" — but do
  not hedge away a well-powered primary result.

## Structure

- **Manuscript / report**: IMRaD. Abstract with structured headings, plain-
  language summary, key points, limitations paragraph, funding and conflicts.
- **Medical information answer**: question restated, short answer, evidence with
  citations, limitations, "consult your clinician" where relevant.
- **Slide notes**: one takeaway per slide, the evidence line, and what to say
  beyond the bullets.
- Keep one term per concept; define the study once and refer to it by acronym.

## Citations

- Vancouver style by default: authors, title, journal, year, volume, pages,
  DOI/PMID. Number in order of first citation.
- Cite the primary publication, not a review of it, for an efficacy claim; cite
  the label/guideline for approved uses.
- Label preprints and conference abstracts; give the data-cut date for
  unpublished analyses.
- Do not invent a citation, DOI, PMID or trial registration number. If a source
  is unavailable, say so and mark the claim as unsupported.

## Privacy, safety and compliance

- No names, initials, exact dates, faces, addresses or rare combinations that
  could re-identify a patient; de-identify case material.
- Do not reproduce promotional claims without substantiation; keep medical-
  affairs and promotional registers distinct.
- Mark investigational, off-label or unapproved uses every time they appear.
- Add the standard disclaimer for the audience: internal draft, not medical
  advice, requires expert review.

## Bilingual Chinese/English

- Use accepted Chinese medical terms, not literal translations: 适应症,
  不良事件 (AE), 严重不良事件 (SAE), 发生率, 终点, 主要终点, 置信区间,
  风险比, 安慰剂对照, 随机双盲.
- Keep drug names in the form the local label uses; give the English name in
  parentheses at first mention when the audience is bilingual.
- Numbers, units and abbreviation expansions follow the target language's
  convention; do not machine-translate a table header without checking units.
- Preserve hedging strength in translation: "may reduce" is 可能降低, not 降低.

## Review checklist

1. Every quantitative claim traces to a source or is labelled as the author's
   calculation.
2. Primary/secondary/post-hoc labels are correct.
3. Abbreviations expanded; units and significant figures consistent.
4. No causal language for observational data; no "significant" without a test.
5. Privacy and unapproved-use labels present.
6. Citations real, correctly ordered, and point at the primary source.
