
import { type ReactNode } from 'react';
import { MeetingState, Decision, ActionItem, Issue, OpenQuestion } from '../../types';

interface IntelligencePanelProps {
  meetingState: MeetingState;
}

// Section heading used throughout the panel.
function SectionHeading({ children }: { children: ReactNode }) {
  return (
    <h3 className="text-xs font-semibold uppercase tracking-wider text-muted mb-2">{children}</h3>
  );
}

// Uniform card body: entity type is carried by a left accent bar, not a
// full-card rainbow fill.
function EntityCard({ accent, children }: { accent: string; children: ReactNode }) {
  return (
    <div className={`p-3 bg-canvas border border-steel/30 border-l-2 rounded-lg ${accent}`}>
      {children}
    </div>
  );
}

export function IntelligencePanel({ meetingState }: IntelligencePanelProps) {
  const { current_topic, topics, decisions, action_items, issues, questions } = meetingState;

  return (
    <div className="flex-1 overflow-y-auto p-4 space-y-6">
      {/* Current Topic */}
      <section>
        <SectionHeading>Current Topic</SectionHeading>
        {current_topic ? (
          <p className="text-ink font-medium">{current_topic}</p>
        ) : (
          <p className="text-muted italic text-sm">No topic detected yet</p>
        )}
      </section>

      {/* Topics */}
      {topics.length > 0 && (
        <section>
          <SectionHeading>Topics Discussed</SectionHeading>
          <div className="flex flex-wrap gap-2">
            {topics.map((topic) => (
              <span
                key={topic.id}
                className="px-2 py-1 bg-brand-100 text-brand-700 text-sm rounded-full"
              >
                {topic.title}
              </span>
            ))}
          </div>
        </section>
      )}

      {/* Decisions */}
      <section>
        <SectionHeading>Decisions ({decisions.length})</SectionHeading>
        {decisions.length === 0 ? (
          <p className="text-muted italic text-sm">No decisions yet</p>
        ) : (
          <div className="space-y-2">
            {decisions.map((decision) => (
              <DecisionCard key={decision.id} decision={decision} />
            ))}
          </div>
        )}
      </section>

      {/* Action Items */}
      <section>
        <SectionHeading>Action Items ({action_items.length})</SectionHeading>
        {action_items.length === 0 ? (
          <p className="text-muted italic text-sm">No action items yet</p>
        ) : (
          <div className="space-y-2">
            {action_items.map((item) => (
              <ActionItemCard key={item.id} item={item} />
            ))}
          </div>
        )}
      </section>

      {/* Issues */}
      <section>
        <SectionHeading>Issues ({issues.length})</SectionHeading>
        {issues.length === 0 ? (
          <p className="text-muted italic text-sm">No issues raised</p>
        ) : (
          <div className="space-y-2">
            {issues.map((issue) => (
              <IssueCard key={issue.id} issue={issue} />
            ))}
          </div>
        )}
      </section>

      {/* Questions */}
      <section>
        <SectionHeading>Open Questions ({questions.length})</SectionHeading>
        {questions.length === 0 ? (
          <p className="text-muted italic text-sm">No open questions</p>
        ) : (
          <div className="space-y-2">
            {questions.map((question) => (
              <QuestionCard key={question.id} question={question} />
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function DecisionCard({ decision }: { decision: Decision }) {
  const statusColors = {
    proposed: 'bg-wash text-ink',
    confirmed: 'bg-brand-100 text-brand-700',
    superseded: 'bg-steel/20 text-muted',
  };

  return (
    <EntityCard accent="border-l-ink">
      <div className="flex items-start justify-between gap-2">
        <p className="text-ink font-medium">{decision.title}</p>
        <span className={`shrink-0 px-2 py-0.5 text-xs rounded-full ${statusColors[decision.status]}`}>
          {decision.status}
        </span>
      </div>
      {decision.description && (
        <p className="mt-1 text-sm text-muted">{decision.description}</p>
      )}
      <div className="mt-2 text-xs text-muted">
        Confidence: {Math.round(decision.confidence * 100)}%
      </div>
    </EntityCard>
  );
}

function ActionItemCard({ item }: { item: ActionItem }) {
  return (
    <EntityCard accent="border-l-brand-500">
      <p className="text-ink">{item.description}</p>
      <div className="mt-2 flex items-center gap-4 text-sm">
        {item.assignee && (
          <span className="text-muted">
            <span className="font-medium text-ink">Assignee:</span> {item.assignee}
          </span>
        )}
        {item.due_date && (
          <span className="text-muted">
            <span className="font-medium text-ink">Due:</span> {item.due_date}
          </span>
        )}
      </div>
    </EntityCard>
  );
}

function IssueCard({ issue }: { issue: Issue }) {
  return (
    <EntityCard accent="border-l-coral-500">
      <p className="text-ink font-medium">{issue.title}</p>
      {issue.description && (
        <p className="mt-1 text-sm text-muted">{issue.description}</p>
      )}
    </EntityCard>
  );
}

function QuestionCard({ question }: { question: OpenQuestion }) {
  return (
    <EntityCard accent="border-l-steel">
      <p className="text-ink">{question.question}</p>
    </EntityCard>
  );
}
