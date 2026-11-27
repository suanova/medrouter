---
name: pharma-trials
description: Reason about and present clinical trial design and results — phases, endpoints, analysis sets, safety reporting, bias traps, and regulated reporting conventions.
---

# Clinical trials and pharmaceutical data

Use for trial design summaries, protocol/SAP questions, study readouts, safety
tables and benefit–risk discussions. For deck mechanics use `medical-slides`;
for prose use `medical-writing`.

Drafting and analysis support only; protocol, statistical and regulatory
decisions require the sponsor's qualified teams. Do not present output as
regulatory advice.

## Terminology that must be exact

- **Phase**: I (safety/PK, often healthy volunteers; oncology often patients),
  II (activity/dose-finding), III (confirmatory, comparative), IV (post-approval).
- **Design**: parallel vs crossover vs single-arm; randomised, blinded (single/
  double), placebo/active comparator; adaptive, seamless, umbrella/basket.
- **Endpoints**: primary, key secondary, exploratory; composite; surrogate vs
  clinical outcome; time-to-event vs proportion vs continuous.
- **Analysis sets**: ITT (all randomised), mITT, per-protocol, safety set
  (treated). Always state which set produced a number.
- **Multiplicity**: hierarchical testing, alpha splitting, alpha spending —
  an unadjusted p-value in a secondary endpoint is not confirmatory.

## What makes a result interpretable

Report and check, in this order:

1. Population and n per arm; disposition and discontinuation reasons.
2. Exposure (dose, duration, follow-up).
3. Primary endpoint: effect size, 95% CI, p, analysis set, timepoint.
4. Key secondary endpoints with the multiplicity strategy.
5. Safety: overall TEAE, treatment-related, grade ≥3, SAE, discontinuations,
   deaths, with the same denominator per arm.
6. Limitations: open-label, single-arm, short follow-up, missing data,
   generalisability.

Do not compare arms across different trials as head-to-head. If a cross-trial
table is requested, label it "cross-trial comparison — not adjusted for
baseline differences".

## Common bias and interpretation traps

- **Immortal time bias** from defining exposure after cohort entry.
- **Subgroup fishing**: only pre-specified subgroups are confirmatory; label
  the rest exploratory and avoid a forest plot of p-values without interaction
  tests.
- **Surrogate endpoints** presented as if they were clinical benefit.
- **Switching / rescue medication** that blurs the randomised comparison.
- **Missing data** handled by complete-case analysis without sensitivity tests.
- **Multiplicity** ignored across many endpoints.
- **Changing the analysis set** between the SAP and the readout.
- **Relative risk without absolute risk**: a 50% relative reduction from 2% to
  1% is a 1-point absolute difference.

## Safety reporting conventions

- MedDRA hierarchy: SOC → PT; use the version and dictionary stated by the study.
- TEAE = event starting or worsening after first dose; do not count pre-existing
  events as TEAE without saying how they were handled.
- `n (%)`, one denominator per column, sorted by frequency then alphabetically.
- Separate deaths, SAEs, related events and events leading to discontinuation.
- Do not describe a safety profile as "safe" or "well tolerated" beyond the
  observed data; state the exposure it is based on.

## Reporting standards

- ICH E3 (clinical study reports), ICH E6/GCP (conduct), ICH E9(R1) (estimands
  and sensitivity analyses).
- CONSORT for randomised trials (flow diagram counts must reconcile),
  PRISMA for systematic reviews, STROBE for observational studies.
- Trial registration ID and data-cut date on every result slide/report.
- Survival: state censoring rule, median vs landmark estimates, and whether the
  proportional-hazards assumption was checked for a hazard ratio.

## Benefit–risk

Present benefits and harms with the same rigour: absolute differences, uncertainty,
reversibility, monitoring burden, and the population in which the data apply.
Distinguish statistical significance from clinical relevance; state the
decision context (regulatory, formulary, clinical pathway).

## Deliverables

- Trial summary: one-page design table + endpoint table + safety table +
  limitations.
- Readout deck: use `medical-slides`; every number traced to the source table.
- Never fabricate a CONSORT count, a KM curve, a forest plot or a p-value. If
  the data are unavailable, mark the slide as a placeholder and say what is
  missing.
