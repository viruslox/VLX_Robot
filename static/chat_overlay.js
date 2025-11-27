// --- Global State Management ---
const mediaQueue = [];
let isPlaying = false;
let basePath = '';

// Calculate master volume (0.0 to 1.0)
const masterVolume = (window.VLX_CONFIG && typeof window.VLX_CONFIG.VOLUME === 'number') 
    ? (window.VLX_CONFIG.VOLUME / 100) 
    : 1.0;

const videoElement = document.getElementById('command-video');

function connect() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    const wsPath = (window.VLX_CONFIG && window.VLX_CONFIG.WEBSOCKET_PATH) || '/vlxrobot/ws';
    basePath = wsPath.substring(0, wsPath.lastIndexOf('/'));

    const socket = new WebSocket(`${protocol}//${host}${wsPath}`);

    socket.onopen = () => console.log("[System] FX Overlay Connected.");
    socket.onclose = (event) => {
        console.warn(`[System] Connection lost. Reconnecting in 5s...`);
        setTimeout(connect, 5000);
    };
    socket.onerror = (e) => {
        console.error("[Error] WebSocket error:", e);
        socket.close();
    };

    socket.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            if (data.type === 'sound_command') {
                mediaQueue.push(data);
                processQueue();
            }
        } catch (err) {
            console.error("[Error] Failed to parse incoming message:", err);
        }
    };
}

function processQueue() {
    if (isPlaying || mediaQueue.length === 0) return;

    isPlaying = true;
    const item = mediaQueue.shift();
    const src = `${basePath}/static/chat/${item.filename}`;

    if (item.media_type === 'video') {
        playVideo(src);
    } else {
        playAudio(src);
    }
}

function playAudio(src) {
    console.log("[Playback] Starting AUDIO:", src);
    const audio = new Audio(src);
    audio.volume = masterVolume; // Apply Volume

    audio.play().catch(e => {
        console.warn("[Warning] Audio playback failed:", e);
        isPlaying = false;
        processQueue();
    });

    audio.onended = () => {
        isPlaying = false;
        processQueue();
    };

    audio.onerror = () => {
        console.error("[Error] Failed to load audio resource:", src);
        isPlaying = false;
        processQueue();
    };
}

function playVideo(src) {
    console.log("[Playback] Starting VIDEO:", src);
    videoElement.src = src;
    videoElement.style.display = 'block';
    videoElement.volume = masterVolume; // Apply Volume

    videoElement.play().catch(e => {
        console.warn("[Warning] Video playback failed:", e);
        videoElement.style.display = 'none';
        isPlaying = false;
        processQueue();
    });

    videoElement.onended = () => {
        videoElement.style.display = 'none';
        videoElement.src = "";
        isPlaying = false;
        processQueue();
    };

    videoElement.onerror = () => {
        console.error("[Error] Failed to load video resource:", src);
        videoElement.style.display = 'none';
        isPlaying = false;
        processQueue();
    };
}

connect();
