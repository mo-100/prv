# AGENTS.md

1. Ask, don't assume. If something is unclear, ask before writing a single line. Never make silent assumptions about intent, architecture, or requirements. When running unattended, pick the most reasonable interpretation, proceed, and record the assumption rather than blocking.

2. Implement the simplest solution for simple problems, better solutions for harder problems. Do not over-engineer or add flexibility that isn't needed yet.

3. Don't touch unrelated code but please do surface bad code or design smells you discover with me so we can address them as a separate issue.

4. Flag uncertainty explicitly. If you're unsure about something, see point 1 above. If it makes sense to do so, conduct a small, localised and low-risk experiment and bring the hypothesis and results to me to discuss. Confidence without certainty causes more damage than admitting a gap.

5. I'm always open to ideas on better ways to do things. Please don't hesitate to suggest a better way, or one that has long lasting impact over a tactical change. (as a few examples)

6. One fact, one home. Knowledge reused in more than one place — a constant, a format, a validation rule, a list of valid values — must live in a single shared definition that every consumer reads from; duplicating it means you're maintaining two copies that will drift apart, which is a bug, not a convenience.

7. Treat existing tests as load-bearing. Don't edit or delete a test case unless I confirm it; when you add a new feature, add tests for it. Prioritise the conflicting scenarios — the edge cases where the code could behave incorrectly — and make sure the behaviour you're encoding is actually intended, not just what happens to pass.
