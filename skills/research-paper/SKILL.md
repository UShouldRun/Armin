---
name: research-paper
description: Help write and revise research papers (systems / hardware / ML venues) — structure, honest claims backed by numbers, related work found with web_search, figures and tables, Typst or LaTeX. Load when working on a paper, abstract or poster text.
---
## Structure
- **Abstract** (150–250 words): problem, why it's hard, the
  approach, headline results with numbers, one line of significance.
- **Introduction**: context → gap → contribution list (3–4 bullets,
  each checkable) → paper outline.
- **Background / related work**: group prior work by approach; say
  how this work differs, not just that others exist.
- **Method**: enough detail to reproduce; diagrams for data flow;
  notation defined once.
- **Evaluation**: setup (hardware, datasets, baselines, metrics,
  seeds) before results; each figure or table answers one question
  stated in the text.
- **Conclusion**: what was shown, limitations, future work.

## Claims
- Every quantitative claim traces to a table, figure or citation.
  Report the baseline next to the improvement ("98% vs. 15% for
  greedy list scheduling"), and variance when runs vary.
- Prefer "reduces X by 33% on our benchmark set" over "significantly
  improves". No result the experiments don't support; flag gaps for
  the user instead of papering over them.
- Author order, affiliations, venue rules and page limits come from
  the user or the call for papers — ask, don't assume.

## Related work
Use `web_search` for candidate papers, then cite only what was
actually found and checked (title, authors, venue, year). Never
invent a citation; mark `[citation needed]` when unsure.

## Writing
- Short sentences, active voice, present tense for what the paper
  does, past for what experiments did.
- Define acronyms at first use (CGRA, DFG, LNS, HLS...).
- Figures: labeled axes with units, readable when printed in
  grayscale, captions that state the takeaway.
- Typst papers: load the typst skill. LaTeX: one sentence per line
  in the source for clean diffs.
- Keep long drafts and reviewer-style notes in the scratchpad; put
  only final text in the paper file.
