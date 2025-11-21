// --- Globals ---
const mediaQueue = [];
let isPlaying = false;
let basePath = '';

const videoElement = document.getElementById('command-video');

// --- WebSocket ---
function connect() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    // Recupera il path del WebSocket dalla configurazione globale se esiste, altrimenti usa il default
    const wsPath = (window.VLX_CONFIG && window.VLX_CONFIG.WEBSOCKET_PATH) || '/vlxrobot/ws';

    // Calcola il percorso base per gli asset (rimuove l'ultimo segmento /ws)
    basePath = wsPath.substring(0, wsPath.lastIndexOf('/'));

    const socket = new WebSocket(`${protocol}//${host}${wsPath}`);

    socket.onopen = () => console.log("FX Overlay Connected.");

    socket.onclose = (event) => {
        console.warn("FX Overlay disconnected. Reconnecting in 5 seconds...", event.reason);
        setTimeout(connect, 5000);
    };

    socket.onerror = (e) => {
        console.error("WebSocket error:", e);
        socket.close();
    };

    socket.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            // Ascolta solo i comandi di tipo 'sound_command'
            if (data.type === 'sound_command') {
                mediaQueue.push(data);
                processQueue();
            }
        } catch (err) { console.error("Error parsing message:", err); }
    };
}

// --- Queue Processor ---
function processQueue() {
    // Se sta già riproducendo qualcosa o la coda è vuota, esce
    if (isPlaying || mediaQueue.length === 0) return;

    isPlaying = true;
    const item = mediaQueue.shift();

    // Costruisce URL: /vlxrobot/static/audio/cartella/file.ext
    // Item filename arriva già formato come "everyone/file.mp3" o "subscribers/file.mp4"
    const src = `${basePath}/static/audio/${item.filename}`;

    if (item.media_type === 'video') {
        playVideo(src);
    } else {
        playAudio(src);
    }
}

function playAudio(src) {
    console.log("Playing AUDIO:", src);
    const audio = new Audio(src);
    audio.volume = 1.0;

    audio.play().catch(e => {
        console.warn("Audio fail:", e);
        isPlaying = false;
        processQueue();
    });

    audio.onended = () => {
        isPlaying = false;
        processQueue();
    };

    audio.onerror = () => {
        console.error("Error loading audio:", src);
        isPlaying = false;
        processQueue();
    };
}

function playVideo(src) {
    console.log("Playing VIDEO:", src);

    // 1. Setup Video
    videoElement.src = src;
    videoElement.style.display = 'block'; // Rendi visibile
    videoElement.volume = 1.0;

    // 2. Play
    videoElement.play().catch(e => {
        console.warn("Video fail:", e);
        videoElement.style.display = 'none';
        isPlaying = false;
        processQueue();
    });

    // 3. Cleanup on End
    videoElement.onended = () => {
        videoElement.style.display = 'none'; // Nascondi di nuovo
        videoElement.src = "";
        isPlaying = false;
        processQueue();
    };

    videoElement.onerror = () => {
        console.error("Error loading video:", src);
        videoElement.style.display = 'none';
        isPlaying = false;
        processQueue();
    };
}

// Avvia la connessione
connect();
