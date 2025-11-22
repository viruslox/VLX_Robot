// --- Global State Management ---
const mediaQueue = [];
let isPlaying = false;
let basePath = '';

// DOM Element Reference
const videoElement = document.getElementById('command-video');

// --- WebSocket Initialization & Event Handling ---
function connect() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;

    // Retrieve the WebSocket endpoint from the global configuration or fall back to the default path.
    const wsPath = (window.VLX_CONFIG && window.VLX_CONFIG.WEBSOCKET_PATH) || '/vlxrobot/ws';

    // Derive the base asset path by stripping the WebSocket endpoint suffix (e.g., removing '/ws').
    basePath = wsPath.substring(0, wsPath.lastIndexOf('/'));

    const socket = new WebSocket(`${protocol}//${host}${wsPath}`);

    socket.onopen = () => console.log("[System] FX Overlay Connected.");

    socket.onclose = (event) => {
        console.warn(`[System] Connection lost (Code: ${event.code}). Reconnecting in 5 seconds...`);
        setTimeout(connect, 5000);
    };

    socket.onerror = (e) => {
        console.error("[Error] WebSocket error observed:", e);
        socket.close();
    };

    socket.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);

            // Filter incoming messages: only process 'sound_command' payloads.
            if (data.type === 'sound_command') {
                mediaQueue.push(data);
                processQueue();
            }
        } catch (err) {
            console.error("[Error] Failed to parse incoming message:", err);
        }
    };
}

// --- Queue Processing Logic ---
function processQueue() {
    // Enforce sequential playback: abort if media is currently playing or if the queue is empty.
    if (isPlaying || mediaQueue.length === 0) return;

    isPlaying = true;
    const item = mediaQueue.shift();

    // Construct the absolute URL for the media asset.
    // The filename property is expected to include the relative subdirectory (e.g., "everyone/file.mp3").
    const src = `${basePath}/static/chat/${item.filename}`;

    if (item.media_type === 'video') {
        playVideo(src);
    } else {
        playAudio(src);
    }
}

/**
 * Handles audio playback logic.
 * @param {string} src - The source URL of the audio file.
 */
function playAudio(src) {
    console.log("[Playback] Starting AUDIO:", src);
    const audio = new Audio(src);
    audio.volume = 1.0;

    // Attempt playback with error handling to prevent queue deadlocks.
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

/**
 * Handles video playback logic and DOM visibility toggling.
 * @param {string} src - The source URL of the video file.
 */
function playVideo(src) {
    console.log("[Playback] Starting VIDEO:", src);

    // 1. Configure Video Element
    videoElement.src = src;
    videoElement.style.display = 'block'; // Ensure visibility during playback
    videoElement.volume = 1.0;

    // 2. Attempt Playback
    videoElement.play().catch(e => {
        console.warn("[Warning] Video playback failed:", e);
        videoElement.style.display = 'none';
        isPlaying = false;
        processQueue();
    });

    // 3. Cleanup upon completion
    videoElement.onended = () => {
        videoElement.style.display = 'none'; // Hide element to maintain transparency
        videoElement.src = ""; // Clear source to release resources
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

// --- Entry Point ---
connect();
