import React from 'react';
import { MeetingState, Decision, ActionItem, Issue, OpenQuestion } from '../../types';

interface IntelligencePanelProps {
  meetingState: MeetingState;
}

export function IntelligencePanel({ meetingState }: IntelligencePanelProps) {
  const { current_topic, topics, decisions, action_items, issues, questions } = meetingState;

  return (
    <div className="flex-1 overflow-y-auto p-4 space-y-6">
      {/* Current Topic */}
      <section>
        <h3 className="text-sm font-medium text-gray-500 mb-2">Current Topic</h3>
        {current_topic ? (
          <p className="text-gray-900 font-medium">{current_topic}</p>
        ) : (
          <p className="text-gray-400 italic">No topic detected yet</p>
        )}
      </section>

      {/* Topics */}
      {topics.length > 0 && (
        <section>
          <h3 className="text-sm font-medium text-gray-500 mb-2">Topics Discussed</h3>
          <div className="flex flex-wrap gap-2">
            {topics.map((topic) => (
              <span
                key={topic.id}
                className="px-2 py-1 bg-blue-50 text-blue-700 text-sm rounded-full"
              >
                {topic.title}
              </span>
            ))}
          </div>
        </section>
      )}

      {/* Decisions */}
      <section>
        <h3 className="text-sm font-medium text-gray-500 mb-2">
          Decisions ({decisions.length})
        </h3>
        {decisions.length === 0 ? (
          <p className="text-gray-400 italic text-sm">No decisions yet</p>
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
        <h3 className="text-sm font-medium text-gray-500 mb-2">
          Action Items ({action_items.length})
        </h3>
        {action_items.length === 0 ? (
          <p className="text-gray-400 italic text-sm">No action items yet</p>
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
        <h3 className="text-sm font-medium text-gray-500 mb-2">
          Issues ({issues.length})
        </h3>
        {issues.length === 0 ? (
          <p className="text-gray-400 italic text-sm">No issues raised</p>
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
        <h3 className="text-sm font-medium text-gray-500 mb-2">
          Open Questions ({questions.length})
        </h3>
        {questions.length === 0 ? (
          <p className="text-gray-400 italic text-sm">No open questions</p>
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
    proposed: 'bg-yellow-100 text-yellow-800',
    confirmed: 'bg-green-100 text-green-800',
    superseded: 'bg-gray-100 text-gray-800',
  };

  return (
    <div className="p-3 bg-green-50 border border-green-200 rounded-lg">
      <div className="flex items-start justify-between">
        <p className="text-gray-900 font-medium">{decision.title}</p>
        <span className={`px-2 py-0.5 text-xs rounded-full ${statusColors[decision.status]}`}>
          {decision.status}
        </span>
      </div>
      {decision.description && (
        <p className="mt-1 text-sm text-gray-600">{decision.description}</p>
      )}
      <div className="mt-2 text-xs text-gray-500">
        Confidence: {Math.round(decision.confidence * 100)}%
      </div>
    </div>
  );
}

function ActionItemCard({ item }: { item: ActionItem }) {
  return (
    <div className="p-3 bg-orange-50 border border-orange-200 rounded-lg">
      <p className="text-gray-900">{item.description}</p>
      <div className="mt-2 flex items-center gap-4 text-sm">
        {item.assignee && (
          <span className="text-gray-600">
            <span className="font-medium">Assignee:</span> {item.assignee}
          </span>
        )}
        {item.due_date && (
          <span className="text-gray-600">
            <span className="font-medium">Due:</span> {item.due_date}
          </span>
        )}
      </div>
    </div>
  );
}

function IssueCard({ issue }: { issue: Issue }) {
  return (
    <div className="p-3 bg-red-50 border border-red-200 rounded-lg">
      <p className="text-gray-900 font-medium">{issue.title}</p>
      {issue.description && (
        <p className="mt-1 text-sm text-gray-600">{issue.description}</p>
      )}
    </div>
  );
}

function QuestionCard({ question }: { question: OpenQuestion }) {
  return (
    <div className="p-3 bg-purple-50 border border-purple-200 rounded-lg">
      <p className="text-gray-900">{question.question}</p>
    </div>
  );
}
