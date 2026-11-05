{{-- The live map's dots (partials/map.blade.php): placed unconditionally on the
     Match page so the function exists when a map appears through a refresh.
     Frames carry the supervisor's read time (at_ms); the map plays them back a
     little behind the newest one and slides between the two around its clock
     on every screen refresh, so uneven answer times don't show as stops. --}}
<script>
    window.echovrLiveMap = window.echovrLiveMap || function (cfg) {
        const NS = 'http://www.w3.org/2000/svg';
        const [minZ, minX, w] = cfg.view;
        const r = cfg.r;
        const delay = Math.max(400, cfg.ms * 2.5); // ms behind the newest frame
        const round = (v) => Math.round(v * 100) / 100;
        let root, timer, raf, stopped = false, fails = 0, busy = false;
        let frames = [], clock = null, lastNow = 0, shown = null;

        const newPlayer = (name) => {
            const g = document.createElementNS(NS, 'g');
            g.dataset.player = name;
            const title = document.createElementNS(NS, 'title');
            title.textContent = name;
            const c = document.createElementNS(NS, 'circle');
            c.setAttribute('cx', 0); c.setAttribute('cy', 0); c.setAttribute('r', round(r * 1.4));
            c.setAttribute('stroke', cfg.label); c.setAttribute('stroke-width', 0);
            const t = document.createElementNS(NS, 'text');
            t.setAttribute('font-size', cfg.font); t.setAttribute('text-anchor', 'middle'); t.setAttribute('fill', cfg.label);
            t.setAttribute('stroke', '#16161a'); t.setAttribute('stroke-width', round(r * 0.5));
            t.setAttribute('paint-order', 'stroke'); t.setAttribute('stroke-linejoin', 'round');
            t.textContent = name.length > 12 ? name.slice(0, 12) + '...' : name;
            g.append(title, c, t);
            const disc = root.querySelector('[data-disc]');
            disc ? root.insertBefore(g, disc) : root.append(g);
            return g;
        };

        const lerp = (a, b, k) => (a && b ? a.map((v, i) => v + (b[i] - v) * k) : a || b);

        // Draws the moment between frames a and b (k from 0 to 1): positions slide,
        // who is there, colours and flags come from the nearer frame.
        const draw = (a, b, k) => {
            const near = k < 0.5 ? a : b;
            const byName = (f) => new Map((f.players || []).filter((p) => p.pos && p.pos.length === 3).map((p) => [p.name, p]));
            const pa = byName(a), pb = byName(b);
            const els = new Map([...root.querySelectorAll('[data-player]')].map((g) => [g.dataset.player, g]));
            const seen = new Set();
            for (const p of near.players || []) {
                if (!p.pos || p.pos.length !== 3) continue;
                seen.add(p.name);
                const g = els.get(p.name) || newPlayer(p.name);
                const pos = lerp((pa.get(p.name) || p).pos, (pb.get(p.name) || p).pos, k);
                const cx = round(pos[2]), cy = round(pos[0]);
                g.style.transform = `translate(${cx}px,${cy}px)`;
                g.setAttribute('opacity', p.stunned ? '.45' : '1');
                const c = g.querySelector('circle');
                c.setAttribute('fill', cfg.colors[p.side] || 'currentColor');
                c.setAttribute('stroke-width', p.possession ? round(r / 2) : 0);
                const t = g.querySelector('text');
                t.setAttribute('x', round(Math.min(Math.max(cx, minZ + r * 5), minZ + w - r * 5) - cx));
                t.setAttribute('y', round(cy - r * 2.4 < minX + r * 2 ? r * 3.8 : -r * 2.4));
            }
            els.forEach((g, name) => { if (!seen.has(name)) g.remove(); });
            const disc = root.querySelector('[data-disc]');
            if (disc) {
                const d = near.disc && near.disc.length === 3 ? lerp(a.disc, b.disc, k) || near.disc : null;
                if (d) {
                    disc.removeAttribute('display');
                    disc.style.transform = `translate(${round(d[2])}px,${round(d[0])}px)`;
                } else {
                    disc.setAttribute('display', 'none');
                }
            }
        };

        const frame = (now) => {
            if (stopped) return;
            raf = requestAnimationFrame(frame);
            if (!frames.length) return;
            const target = frames[frames.length - 1].at_ms - delay;
            if (clock === null || Math.abs(target - clock) > 1500) {
                clock = target; // first frame, or far off (tab was hidden): jump
            } else {
                // Run with the local clock, pulled gently towards the target so
                // the buffer neither runs dry nor grows.
                clock += (now - lastNow) + (target - clock) * 0.02;
            }
            lastNow = now;
            while (frames.length > 2 && frames[1].at_ms <= clock) frames.shift();
            const a = frames[0], b = frames[1] || frames[0];
            const span = b.at_ms - a.at_ms;
            const k = span > 0 ? Math.min(1, Math.max(0, (clock - a.at_ms) / span)) : 1;
            const key = a.at_ms + ':' + b.at_ms + ':' + k.toFixed(3);
            if (key !== shown) { shown = key; draw(a, b, k); }
        };

        const poll = async () => {
            if (stopped || busy) return;
            if (document.hidden) { frames = []; clock = null; return; }
            busy = true;
            try {
                const res = await fetch(cfg.url, { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } });
                if (res.status === 200) {
                    const live = await res.json();
                    const last = frames[frames.length - 1];
                    if (last && live.session_id !== last.session_id) { frames = []; clock = null; }
                    if (!frames.length || live.at_ms > frames[frames.length - 1].at_ms) {
                        frames.push(live);
                        if (frames.length > 40) frames.shift();
                    }
                } else if (res.status !== 204) {
                    throw new Error('HTTP ' + res.status);
                }
                fails = 0;
            } catch (e) {
                fails++;
            } finally {
                busy = false;
            }
        };

        // Asks at a steady pace (not "wait after each answer"), skipping a turn
        // while an answer is still out; backs off after errors.
        const schedule = () => {
            if (stopped) return;
            poll();
            timer = setTimeout(schedule, fails ? Math.min(5000, cfg.ms * 2 ** fails) : cfg.ms);
        };

        return {
            init() { root = this.$el; schedule(); raf = requestAnimationFrame((t) => { lastNow = t; frame(t); }); },
            destroy() { stopped = true; clearTimeout(timer); cancelAnimationFrame(raf); },
        };
    };
</script>
