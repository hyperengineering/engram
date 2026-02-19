# Recall Integration Template for Agent Definitions

This template provides the patterns needed to integrate Engram/Recall memory into AI agent definitions. It can be adapted for any agent framework that uses structured prompts or XML-based agent definitions.

## Overview

The recall system provides four MCP tools:
- `recall_query` - Retrieve relevant lore at workflow start
- `recall_feedback` - Rate retrieved lore at workflow end
- `recall_record` - Capture validated learnings (restricted to designated agents)
- `recall_sync` - Push local changes to central Engram service

## Core Design Principles

1. **Query early** - Retrieve context before making decisions
2. **Feedback always** - Rate every piece of retrieved lore
3. **Record sparingly** - Only record after implementation validates the learning
4. **Sync at boundaries** - Push to central service at natural workflow boundaries

---

## XML Block Template

Embed this block in agent activation/configuration sections:

```xml
<recall-integration>
  <description>
    Engram memory integration via Recall MCP tools.
    Query lore at workflow start. Provide feedback at workflow end.
    [Recording policy specific to this agent role]
  </description>

  <session-state>
    Track retrieved lore references (L1, L2, ...) during session for feedback.
    Variable: {recalled_lore_refs} — populated by recall_query results
  </session-state>

  <categories role="[ROLE_NAME]">
    <query>[COMMA_SEPARATED_CATEGORIES]</query>
    <record>[COMMA_SEPARATED_CATEGORIES or "none"]</record>
  </categories>

  <workflow-hooks>
    <hook phase="workflow_start" applies-to="[workflow-ids]">
      Before beginning [activity], query Engram for relevant context:
      recall_query(
        query: "[domain] [component] [relevant-keywords]",
        k: 5,
        min_confidence: 0.5,
        categories: ["CATEGORY_1", "CATEGORY_2"]
      )
      Store returned refs in {recalled_lore_refs} for later feedback.
      Consider retrieved lore when [making decisions / planning approach].
    </hook>

    <hook phase="workflow_end" applies-to="[workflow-ids]">
      If {recalled_lore_refs} is not empty, assess each piece of retrieved lore:
      - helpful: Directly informed a better [decision/implementation/review]
      - not_relevant: Retrieved but did not apply to this context
      - incorrect: Was wrong or would have led to [problems]

      recall_feedback(helpful: [...], not_relevant: [...], incorrect: [...])
      Clear {recalled_lore_refs} after feedback.
    </hook>

    <!-- Include only for recording agents -->
    <hook phase="record" applies-to="[workflow-ids]">
      After [validation event], record learning:
      recall_record(
        content: [what was learned - specific and actionable],
        category: "[CATEGORY]",
        context: [reference to story/epic/project],
        confidence: [0.5-0.8 based on validation strength]
      )
    </hook>

    <!-- Include only for sync-responsible agents -->
    <hook phase="sync" applies-to="[workflow-ids]">
      At [boundary event], sync to central Engram:
      recall_sync()
    </hook>
  </workflow-hooks>

  <recording-note>
    [Agent name] [does/does NOT] record directly to Engram.
    [Explanation of recording policy]
  </recording-note>

  <confidence-guidance>
    When providing feedback:
    - helpful: Lore that [specific positive outcome]
    - not_relevant: Lore about [wrong context examples]
    - incorrect: Lore that [specific negative outcome]

    When recording (if applicable):
    0.5-0.6: Single validated instance
    0.7-0.8: Multiple validations or team consensus
    0.9+: Established pattern, proven repeatedly
  </confidence-guidance>
</recall-integration>
```

---

## Lore Categories Reference

Choose categories based on agent role:

| Category | Description | Typical Agents |
|----------|-------------|----------------|
| `ARCHITECTURAL_DECISION` | System design choices, ADRs, structural decisions | Architect, Tech Lead |
| `PATTERN_OUTCOME` | Results of applying design patterns (success/failure) | Architect, Developer, Reviewer |
| `INTERFACE_LESSON` | API contracts, integration points, boundary behaviors | Architect, Developer |
| `EDGE_CASE_DISCOVERY` | Unexpected behaviors, boundary conditions found | Developer, Reviewer, QA |
| `IMPLEMENTATION_FRICTION` | Difficulties encountered during implementation | Developer, Refactor |
| `TESTING_STRATEGY` | Effective testing approaches, coverage insights | Developer, Reviewer, QA |
| `DEPENDENCY_BEHAVIOR` | Third-party library quirks, version issues | Developer |
| `PERFORMANCE_INSIGHT` | Optimization learnings, bottleneck discoveries | Developer, Performance |

---

## Role Templates

### Architect / Design Agent (Records validated learnings)

```xml
<recall-integration>
  <description>
    Engram memory integration via Recall MCP tools.
    Query lore at workflow start. Provide feedback at workflow end.
    Record only validated learnings after implementation confirms design.
  </description>

  <session-state>
    Track retrieved lore references (L1, L2, ...) during session for feedback.
    Variable: {recalled_lore_refs} — populated by recall_query results
  </session-state>

  <categories role="architecture">
    <query>ARCHITECTURAL_DECISION, PATTERN_OUTCOME, INTERFACE_LESSON</query>
    <record>ARCHITECTURAL_DECISION, PATTERN_OUTCOME, INTERFACE_LESSON</record>
  </categories>

  <workflow-hooks>
    <hook phase="workflow_start" applies-to="design,analysis,consultation">
      Before beginning analysis, query Engram for relevant context:
      recall_query(
        query: "[domain] architectural decisions patterns interfaces",
        k: 5,
        min_confidence: 0.5,
        categories: ["ARCHITECTURAL_DECISION", "PATTERN_OUTCOME", "INTERFACE_LESSON"]
      )
      Store returned refs in {recalled_lore_refs} for later feedback.
      Consider retrieved lore when making design decisions.
    </hook>

    <hook phase="workflow_end" applies-to="design,analysis,consultation">
      If {recalled_lore_refs} is not empty, assess each piece of retrieved lore:
      - helpful: Directly informed a better design decision
      - not_relevant: Retrieved but did not apply to this context
      - incorrect: Was wrong or would have led to suboptimal design

      recall_feedback(helpful: [...], not_relevant: [...], incorrect: [...])
      Clear {recalled_lore_refs} after feedback.
    </hook>

    <hook phase="record" applies-to="record-decision">
      Explicit decisions (ADRs) are recorded immediately after documentation:
      recall_record(
        content: [formatted decision: context, options, decision, rationale],
        category: "ARCHITECTURAL_DECISION",
        context: [project/epic reference],
        confidence: 0.7
      )
    </hook>

    <hook phase="feedback_loop" applies-to="post-implementation-review">
      Compare design to implementation. For each validated learning:
      recall_record(
        content: [what was learned - specific and actionable],
        category: [PATTERN_OUTCOME | INTERFACE_LESSON | IMPLEMENTATION_FRICTION],
        context: [story/epic reference],
        confidence: [0.6-0.8 based on validation strength]
      )
      recall_sync()
    </hook>
  </workflow-hooks>

  <recording-note>
    This agent IS the designated recorder for the team.
    ADRs are recorded immediately (explicit team decisions).
    Other learnings are recorded only after implementation validates them.
  </recording-note>

  <confidence-guidance>
    0.5-0.6: Single validated instance (design worked once)
    0.7-0.8: Multiple validations or team consensus
    0.9+: Established pattern, proven repeatedly
  </confidence-guidance>
</recall-integration>
```

### Developer Agent (Query and feedback only)

```xml
<recall-integration>
  <description>
    Engram memory integration via Recall MCP tools.
    Query lore at workflow start for implementation guidance.
    Provide feedback at workflow end.
    Recording happens via designated recorder after implementation validates learnings.
  </description>

  <session-state>
    Track retrieved lore references (L1, L2, ...) during session for feedback.
    Variable: {recalled_lore_refs} — populated by recall_query results
  </session-state>

  <categories role="developer">
    <query>IMPLEMENTATION_FRICTION, EDGE_CASE_DISCOVERY, DEPENDENCY_BEHAVIOR, TESTING_STRATEGY</query>
    <record>none</record>
  </categories>

  <workflow-hooks>
    <hook phase="workflow_start" applies-to="implement,debug,explore">
      Before beginning implementation, query Engram for relevant context:
      recall_query(
        query: "[domain] [component] implementation friction edge cases dependencies",
        k: 5,
        min_confidence: 0.5,
        categories: ["IMPLEMENTATION_FRICTION", "EDGE_CASE_DISCOVERY", "DEPENDENCY_BEHAVIOR", "TESTING_STRATEGY"]
      )
      Store returned refs in {recalled_lore_refs} for later feedback.
      Consider retrieved lore when planning implementation approach.
    </hook>

    <hook phase="workflow_end" applies-to="implement,handoff">
      If {recalled_lore_refs} is not empty, assess each piece of retrieved lore:
      - helpful: Directly informed better implementation decisions
      - not_relevant: Retrieved but did not apply to this context
      - incorrect: Was wrong or would have led to bugs

      recall_feedback(helpful: [...], not_relevant: [...], incorrect: [...])
      Clear {recalled_lore_refs} after feedback.
    </hook>
  </workflow-hooks>

  <recording-note>
    This agent does NOT record directly to Engram.
    Implementation learnings are captured by the designated recorder after merge,
    ensuring only validated knowledge enters the knowledge base.
    Document deviations and discoveries in story/task file for harvesting.
  </recording-note>

  <confidence-guidance>
    When providing feedback:
    - helpful: Lore that prevented a bug or guided a better approach
    - not_relevant: Lore about different context (wrong component, different pattern)
    - incorrect: Lore that was outdated or would have caused issues
  </confidence-guidance>
</recall-integration>
```

### Reviewer / QA Agent (Query and feedback only)

```xml
<recall-integration>
  <description>
    Engram memory integration via Recall MCP tools.
    Query lore at review start for quality patterns and past findings.
    Provide feedback at workflow end.
    Recording happens via designated recorder after merge validates learnings.
  </description>

  <session-state>
    Track retrieved lore references (L1, L2, ...) during session for feedback.
    Variable: {recalled_lore_refs} — populated by recall_query results
  </session-state>

  <categories role="reviewer">
    <query>TESTING_STRATEGY, PATTERN_OUTCOME, INTERFACE_LESSON, EDGE_CASE_DISCOVERY</query>
    <record>none</record>
  </categories>

  <workflow-hooks>
    <hook phase="workflow_start" applies-to="review,test,validate">
      Before beginning review, query Engram for relevant context:
      recall_query(
        query: "[component] [pattern] testing strategies quality patterns past issues",
        k: 5,
        min_confidence: 0.5,
        categories: ["TESTING_STRATEGY", "PATTERN_OUTCOME", "INTERFACE_LESSON", "EDGE_CASE_DISCOVERY"]
      )
      Store returned refs in {recalled_lore_refs} for later feedback.
      Consider retrieved lore when assessing quality and design alignment.
    </hook>

    <hook phase="workflow_end" applies-to="review,handoff">
      If {recalled_lore_refs} is not empty, assess each piece of retrieved lore:
      - helpful: Directly informed review findings or caught an issue
      - not_relevant: Retrieved but did not apply to this review
      - incorrect: Was outdated or would have led to wrong feedback

      recall_feedback(helpful: [...], not_relevant: [...], incorrect: [...])
      Clear {recalled_lore_refs} after feedback.
    </hook>
  </workflow-hooks>

  <recording-note>
    This agent does NOT record directly to Engram.
    Review insights are captured by the designated recorder after merge,
    ensuring only validated knowledge enters the knowledge base.
    Document significant findings in story/task file for harvesting.
  </recording-note>

  <confidence-guidance>
    When providing feedback:
    - helpful: Lore that helped identify a real issue or validate quality
    - not_relevant: Lore about different patterns or components
    - incorrect: Lore that suggested wrong quality standards
  </confidence-guidance>
</recall-integration>
```

### Orchestrator / Delivery Agent (Sync responsibility)

```xml
<recall-integration>
  <description>
    Engram memory integration via Recall MCP tools.
    Query lore at workflow start for patterns and solutions.
    Provide feedback at workflow end.
    Sync at delivery — natural boundary before merge.
    Recording happens via designated recorder after merge validates learnings.
  </description>

  <session-state>
    Track retrieved lore references (L1, L2, ...) during session for feedback.
    Variable: {recalled_lore_refs} — populated by recall_query results
  </session-state>

  <categories role="orchestrator">
    <query>PATTERN_OUTCOME, IMPLEMENTATION_FRICTION, TESTING_STRATEGY</query>
    <record>none</record>
  </categories>

  <workflow-hooks>
    <hook phase="workflow_start" applies-to="coordinate,refine,finalize">
      Before beginning work, query Engram for relevant context:
      recall_query(
        query: "[component] patterns solutions coordination",
        k: 5,
        min_confidence: 0.5,
        categories: ["PATTERN_OUTCOME", "IMPLEMENTATION_FRICTION"]
      )
      Store returned refs in {recalled_lore_refs} for later feedback.
      Consider retrieved lore when making coordination decisions.
    </hook>

    <hook phase="workflow_end" applies-to="coordinate,deliver">
      If {recalled_lore_refs} is not empty, assess each piece of retrieved lore:
      - helpful: Directly informed better decisions
      - not_relevant: Retrieved but did not apply to this work
      - incorrect: Was outdated or suggested wrong approach

      recall_feedback(helpful: [...], not_relevant: [...], incorrect: [...])
      Clear {recalled_lore_refs} after feedback.
    </hook>

    <hook phase="sync" applies-to="deliver,complete-sprint">
      At delivery/sprint boundary, sync accumulated learnings to central Engram:
      recall_sync()
      This ensures all feedback from the pipeline is persisted
      before merge triggers the designated recorder's feedback loop.
    </hook>
  </workflow-hooks>

  <recording-note>
    This agent does NOT record directly to Engram.
    This agent IS responsible for triggering sync at delivery boundaries.
    Document opportunities and insights in story/task file for harvesting.
  </recording-note>

  <confidence-guidance>
    When providing feedback:
    - helpful: Lore that guided effective coordination or problem-solving
    - not_relevant: Lore about different contexts or components
    - incorrect: Lore that suggested approaches that would have caused issues
  </confidence-guidance>
</recall-integration>
```

---

## Prompt/Workflow Step Templates

Embed these step patterns into workflow prompts:

### Query Step (at workflow start)

```markdown
1. [RECALL] Query Engram for relevant context:
   - recall_query(query: "[domain] [keywords]", categories: ["CAT1", "CAT2"])
   - Store refs in {recalled_lore_refs}, consider retrieved context
```

### Feedback Step (at workflow end)

```markdown
N. [RECALL] Provide feedback on retrieved lore:
   - recall_feedback(helpful: [...], not_relevant: [...], incorrect: [...])
```

### Record Step (for designated recorder only, after validation)

```markdown
N. [RECALL] Record validated learning to Engram:
   - recall_record(
       content: [what was learned - specific and actionable],
       category: "[CATEGORY]",
       context: [story/epic reference],
       confidence: [0.6-0.8]
     )
```

### Sync Step (at boundaries)

```markdown
N. [RECALL] Sync to central Engram:
   - recall_sync()
```

---

## Team Configuration Patterns

### Single-Agent Setup

One agent handles all roles:
- Query at workflow start
- Provide feedback at workflow end
- Record validated learnings
- Sync at session end

### Multi-Agent Pipeline

Designate one agent as the "recorder" (typically Architect or Tech Lead):

| Role | Query | Feedback | Record | Sync |
|------|-------|----------|--------|------|
| Architect | ✅ | ✅ | ✅ | ✅ |
| Developer | ✅ | ✅ | ❌ | ❌ |
| Reviewer | ✅ | ✅ | ❌ | ❌ |
| Orchestrator | ✅ | ✅ | ❌ | ✅ |

Non-recording agents document discoveries in shared artifacts (story files, PR descriptions) for the recorder to harvest during their feedback loop.

---

## Environment Setup

Required environment variables for sync:

```bash
ENGRAM_URL=https://your-engram-instance.com
ENGRAM_API_KEY=your-api-key

# Optional: project isolation
ENGRAM_STORE_ID=org/team/project
```

---

## MCP Tool Signatures

### recall_query

```
recall_query(
  query: string,           # Semantic search query
  k?: number,              # Max results (default: 5)
  min_confidence?: number, # Threshold 0.0-1.0 (default: 0.5)
  categories?: string[]    # Filter by category
)
→ Returns lore entries with session refs (L1, L2, ...)
```

### recall_feedback

```
recall_feedback(
  helpful?: string[],      # Session refs that helped (L1, L2)
  not_relevant?: string[], # Session refs not applicable
  incorrect?: string[]     # Session refs that were wrong
)
→ Adjusts confidence: helpful +0.08, incorrect -0.15
```

### recall_record

```
recall_record(
  content: string,         # What was learned (max 4000 chars)
  category: string,        # One of the 8 categories
  context?: string,        # Story/epic/project reference
  confidence?: number      # Initial confidence 0.0-1.0 (default: 0.5)
)
→ Creates new lore entry
```

### recall_sync

```
recall_sync()
→ Pushes pending local changes to Engram central service
```
