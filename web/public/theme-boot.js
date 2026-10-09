// Applies the saved theme before first paint (no flash). CSP-safe: a file, not inline.
try { var t = localStorage.getItem("rp:theme"); if (t === "light" || t === "dark") document.documentElement.dataset.theme = t; } catch (e) {}
