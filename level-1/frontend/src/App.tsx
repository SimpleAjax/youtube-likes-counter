import { useEffect, useRef, useState } from 'react';

const videos = [
  { id: '11111111-1111-4111-8111-111111111111', label: 'The first video' },
  { id: '22222222-2222-4222-8222-222222222222', label: 'A second video' },
  { id: '33333333-3333-4333-8333-333333333333', label: 'A third video' },
  { id: '44444444-4444-4444-8444-444444444444', label: 'A fourth video' },
  { id: '55555555-5555-4555-8555-555555555555', label: 'A fifth video' },
];

type Video = {
  id: string;
  title: string;
  likeCount: number;
  likedByViewer: boolean;
};

function getViewerId(): string {
  const key = 'likes-demo-viewer-id';
  let id = localStorage.getItem(key);
  if (!id) {
    id = crypto.randomUUID();
    localStorage.setItem(key, id);
  }
  return id;
}

const viewerId = getViewerId();

async function requestVideo(videoId: string, method = 'GET'): Promise<Video> {
  const response = await fetch(`/api/videos/${videoId}${method === 'GET' ? '' : '/like'}`, {
    method,
    headers: { 'X-Demo-Viewer-Id': viewerId },
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `Request failed (${response.status})`);
  }
  return response.json();
}

export default function App() {
  const [selectedId, setSelectedId] = useState(videos[0].id);
  const [video, setVideo] = useState<Video | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const requestGeneration = useRef(0);
  const mutationInFlight = useRef(false);

  useEffect(() => {
    let active = true;
    setVideo(null);
    setError('');
    requestGeneration.current += 1;

    const refresh = async () => {
      if (mutationInFlight.current) return;
      const generation = requestGeneration.current;
      try {
        const next = await requestVideo(selectedId);
        if (active && generation === requestGeneration.current) {
          setVideo(next);
          setError('');
        }
      } catch (cause) {
        if (active && generation === requestGeneration.current) setError(cause instanceof Error ? cause.message : 'Could not load the video');
      }
    };

    void refresh();
    const timer = window.setInterval(() => void refresh(), 2000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [selectedId]);

  const toggleLike = async () => {
    if (!video || busy) return;
    mutationInFlight.current = true;
    requestGeneration.current += 1;
    setBusy(true);
    setError('');
    try {
      const updated = await requestVideo(video.id, video.likedByViewer ? 'DELETE' : 'PUT');
      setVideo((current) => current?.id === updated.id ? updated : current);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not update the like');
    } finally {
      mutationInFlight.current = false;
      setBusy(false);
    }
  };

  return (
    <main className="shell">
      <header className="topbar">
        <span className="brand-mark">▶</span>
        <span>LIKE COUNTER</span>
        <span className="level">LEVEL 01 / SMALL LAUNCH</span>
      </header>

      <section className="intro">
        <p className="eyebrow">A tiny feature under the microscope</p>
        <h1>One click.<br /><em>One database.</em></h1>
        <p>Pick a video and press Like. This first version counts active likes directly in PostgreSQL. The number refreshes every two seconds so you can watch it change from another browser.</p>
      </section>

      <section className="panel" aria-label="Video likes demo">
        <div className="panel-heading">
          <div>
            <span className="step">01 / LIVE DEMO</span>
            <h2>The counter</h2>
          </div>
          <span className="status"><span className="status-dot" /> {error ? 'API UNAVAILABLE' : 'LOCAL SYSTEM'}</span>
        </div>

        <label className="selector-label" htmlFor="video-select">VIDEO</label>
        <select id="video-select" value={selectedId} onChange={(event) => setSelectedId(event.target.value)}>
          {videos.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}
        </select>

        <div className="video-card">
          <div className="play">▶</div>
          <div>
            <span className="video-kicker">DEMO VIDEO</span>
            <h3>{video?.title ?? 'Loading…'}</h3>
          </div>
        </div>

        <div className="counter-row">
          <div>
            <span className="counter-label">CURRENT LIKES</span>
            <strong aria-live="polite">{video ? video.likeCount.toLocaleString() : '—'}</strong>
          </div>
          <button type="button" onClick={() => void toggleLike()} disabled={!video || busy} aria-pressed={video?.likedByViewer ?? false}>
            <span aria-hidden="true">♥</span> {video?.likedByViewer ? 'Unlike' : 'Like'}
          </button>
        </div>
        {error && <p className="error" role="alert">{error}</p>}
      </section>

      <footer>ONE API INSTANCE <span>·</span> ONE POSTGRES DATABASE <span>·</span> NO CACHE</footer>
    </main>
  );
}
