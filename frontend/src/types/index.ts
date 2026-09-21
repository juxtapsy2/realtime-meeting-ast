export interface Meeting {
  id: number;
  title: string;
  project_id?: number;
  status: 'pending' | 'active' | 'completed';
  started_at?: string;
  ended_at?: string;
  created_at: string;
  updated_at: string;
}

export interface TranscriptEvent {
  meeting_id: string;
  segment_id: string;
  text: string;
  speaker_id?: string;
  start_time: number;
  end_time: number;
  confidence?: number;
  final: boolean;
}

export interface Decision {
  id: string;
  title: string;
  description?: string;
  status: 'proposed' | 'confirmed' | 'superseded';
  confidence: number;
  source_segment_ids?: string[];
}

export interface ActionItem {
  id: string;
  description: string;
  assignee?: string;
  due_date?: string;
  status: 'pending' | 'completed';
  source_segment_ids?: string[];
}

export interface Issue {
  id: string;
  title: string;
  description?: string;
  status: 'open' | 'resolved';
  source_segment_ids?: string[];
}

export interface OpenQuestion {
  id: string;
  question: string;
  status: 'open' | 'resolved';
  source_segment_ids?: string[];
}

export interface MeetingState {
  current_topic: string;
  topics: Array<{ id: string; title: string; keywords?: string[] }>;
  decisions: Decision[];
  action_items: ActionItem[];
  issues: Issue[];
  questions: OpenQuestion[];
}

export interface WebSocketEvent {
  type: string;
  data: any;
}
