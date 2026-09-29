import { useState, useCallback, useRef, useEffect } from 'react';
import { Meeting, TranscriptEvent, MeetingState } from '../../types';
import { useWebSocket } from '../../hooks/useWebSocket';
import { useMicrophone } from '../../hooks/useMicrophone';
import { TranscriptPanel } from '../transcript/TranscriptPanel';
import { IntelligencePanel } from '../intelligence/IntelligencePanel';

interface MeetingRoomProps {
  meeting: Meeting;
  onBack: () => void;
  onMeetingEnded?: () => void;
}

export function MeetingRoom({ meeting, onBack, onMeetingEnded }: MeetingRoomProps) {
  const [transcript, setTranscript] = useState<TranscriptEvent[]>([]);
  const [partialText, setPartialText] = useState<string>('');
  const [meetingState, setMeetingState] = useState<MeetingState>({
    current_topic: '',
    topics: [],
    decisions: [],
    action_items: [],
    issues: [],
    questions: [],
  });
  const [isMeetingActive, setIsMeetingActive] = useState(meeting.status === 'active');

  const handleTranscriptPartial = useCallback((event: TranscriptEvent) => {
    setPartialText(event.text);
  }, []);

  const handleTranscriptFinal = useCallback((event: TranscriptEvent) => {
    setTranscript((prev) => [...prev, event]);
    setPartialText('');
  }, []);

  const handleMeetingStarted = useCallback(() => {
    setIsMeetingActive(true);
  }, []);

  const stopMicRef = useRef<() => void>(() => {});
  const stopMicRefSetter = useCallback((cb: () => void) => {
    stopMicRef.current = cb;
  }, []);

  const handleMeetingEnded = useCallback(() => {
    setIsMeetingActive(false);
    stopMicRef.current();
    onMeetingEnded?.();
  }, [onMeetingEnded]);

  const handleStateUpdate = useCallback((state: Partial<MeetingState>) => {
    setMeetingState((prev) => ({
      ...prev,
      ...state,
    }));
  }, []);

  const { isConnected, sendAudio, sendCommand } = useWebSocket({
    meetingId: meeting.id,
    onTranscriptPartial: handleTranscriptPartial,
    onTranscriptFinal: handleTranscriptFinal,
    onMeetingStarted: handleMeetingStarted,
    onMeetingEnded: handleMeetingEnded,
    onStateUpdate: handleStateUpdate,
  });

  const handleAudioData = useCallback((data: ArrayBuffer) => {
    sendAudio(data);
  }, [sendAudio]);

  const { isActive: isMicActive, source: audioSource, start: startMic, stop: stopMic } = useMicrophone({
    onAudioData: handleAudioData,
  });

  useEffect(() => {
    stopMicRefSetter(stopMic);
  }, [stopMic, stopMicRefSetter]);

  const handleStartMeeting = () => {
    sendCommand('start_meeting', { meeting_id: meeting.id });
  };

  const handleEndMeeting = () => {
    sendCommand('end_meeting', { meeting_id: meeting.id });
  };

  const handleToggleMic = async () => {
    if (isMicActive) {
      stopMic();
    } else {
      await startMic('microphone');
    }
  };

  const handleToggleSystemAudio = async () => {
    if (isMicActive) {
      stopMic();
    } else {
      await startMic('system');
    }
  };

  return (
    <div className="h-screen flex flex-col bg-gray-50">
      {/* Header */}
      <header className="bg-white border-b border-gray-200 px-6 py-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-4">
            <button
              onClick={onBack}
              className="text-gray-500 hover:text-gray-700"
            >
              <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 19l-7-7m0 0l7-7m-7 7h18" />
              </svg>
            </button>
            <div>
              <h1 className="text-xl font-semibold text-gray-900">{meeting.title}</h1>
              <p className="text-sm text-gray-500">
                Status: {isMeetingActive ? 'Recording' : meeting.status}
              </p>
            </div>
          </div>

          <div className="flex items-center gap-4">
            {/* Connection status */}
            <div className="flex items-center gap-2">
              <div className={`w-2 h-2 rounded-full ${isConnected ? 'bg-green-500' : 'bg-red-500'}`} />
              <span className="text-sm text-gray-500">
                {isConnected ? 'Connected' : 'Disconnected'}
              </span>
            </div>

            {/* Meeting controls */}
            {!isMeetingActive ? (
              <button
                onClick={handleStartMeeting}
                disabled={!isConnected}
                className="px-4 py-2 bg-green-600 text-white rounded-lg hover:bg-green-700 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                Start Meeting
              </button>
            ) : (
              <button
                onClick={handleEndMeeting}
                className="px-4 py-2 bg-red-600 text-white rounded-lg hover:bg-red-700"
              >
                End Meeting
              </button>
            )}

            {/* Audio source control */}
            <button
              onClick={handleToggleMic}
              disabled={!isConnected || !isMeetingActive}
              className={`px-4 py-2 rounded-lg disabled:opacity-50 disabled:cursor-not-allowed ${
                isMicActive && audioSource === 'microphone'
                  ? 'bg-red-100 text-red-700 hover:bg-red-200'
                  : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
              }`}
            >
              {isMicActive && audioSource === 'microphone' ? 'Stop Mic' : 'Use Mic'}
            </button>
            <button
              onClick={handleToggleSystemAudio}
              disabled={!isConnected || !isMeetingActive}
              className={`px-4 py-2 rounded-lg disabled:opacity-50 disabled:cursor-not-allowed ${
                isMicActive && audioSource === 'system'
                  ? 'bg-red-100 text-red-700 hover:bg-red-200'
                  : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
              }`}
            >
              {isMicActive && audioSource === 'system' ? 'Stop System Audio' : 'Use System Audio'}
            </button>
          </div>
        </div>
      </header>

      {/* Main content */}
      <div className="flex-1 flex overflow-hidden">
        {/* Transcript panel */}
        <div className="flex-1 flex flex-col border-r border-gray-200">
          <div className="px-4 py-3 bg-white border-b border-gray-200">
            <h2 className="text-lg font-medium text-gray-900">Live Transcript</h2>
          </div>
          <TranscriptPanel
            transcript={transcript}
            partialText={partialText}
          />
        </div>

        {/* Intelligence panel */}
        <div className="w-96 flex flex-col bg-white">
          <div className="px-4 py-3 border-b border-gray-200">
            <h2 className="text-lg font-medium text-gray-900">AI Insights</h2>
          </div>
          <IntelligencePanel meetingState={meetingState} />
        </div>
      </div>
    </div>
  );
}
