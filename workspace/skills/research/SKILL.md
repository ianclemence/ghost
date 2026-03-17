# Research & Reporting Skill

This skill enables Ghost to perform deep, multi-stage industry research and generate professional PDF reports using LaTeX.

## Capabilities

- **Structured Investigation**: Follows the "Strategy -> Execution -> Synthesis -> Report" workflow.
- **Data Grounding**: Uses `web_search` and `web_fetch` with mandatory citation requirements.
- **Professional Formatting**: Generates LaTeX source code using the provided template for high-quality PDF output.

## Workflow

1.  **Define Strategy**: Brainstorm search terms and identify key metrics to track.
2.  **Gather Evidence**: Execute multiple `web_search` queries and fetch detailed content from relevant sources.
3.  **Synthesize Insights**: Use the "phenomenon–cause–impact–solution" chain. Mark strategic observations with **【Insight】**.
4.  **Generate Report**:
    - Use the template in `templates/report.tex`.
    - Fill in the findings, data tables, and citations.
    - Save the `.tex` file to `workspace/tmp/`.
    - (Optional) If `xelatex` is available on the system, compile to PDF.

## Template Location

The professional LaTeX template is located at:
`workspace/skills/research/templates/report.tex`

## Operational Rules

- **Strict Citations**: Every data point must be traceable.
- **Visual Standards**: Use the professional color scheme (mist-blue `#004C8C`).
- **Unified Language**: The report must match the language of the user's request.
