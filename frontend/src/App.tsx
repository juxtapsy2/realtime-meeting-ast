import { useState } from 'react';
import { Meeting } from './types';
import { MeetingList } from './features/meetings/MeetingList';
import { MeetingRoom } from './features/meetings/MeetingRoom';

function App() {
  const [selectedMeeting, setSelectedMeeting] = useState<Meeting | null>(null);

  if (selectedMeeting) {
    return (
      <MeetingRoom
        meeting={selectedMeeting}
        onBack={() => setSelectedMeeting(null)}
      />
    );
  }

  return (
    <div className="min-h-screen bg-gray-50">
      <MeetingList onSelectMeeting={setSelectedMeeting} />
    </div>
  );
}

export default App;
