"""Synthesizes the showreel's music bed and sound effects into media/ (stdlib only).

Run: python tools/gen_audio.py
The bed is 120 BPM (a beat every 0.5 s) so cuts and hits in src/lib.rs land on the grid.
"""
import math
import random
import struct
import wave
from pathlib import Path

SR = 48000
OUT = Path(__file__).resolve().parent.parent / "media"
rng = random.Random(7)


def write(name, left, right=None):
    right = right if right is not None else left
    peak = max(1e-9, max(max(abs(x) for x in left), max(abs(x) for x in right)))
    k = min(1.0, 0.89 / peak)
    with wave.open(str(OUT / name), "wb") as w:
        w.setnchannels(2)
        w.setsampwidth(2)
        w.setframerate(SR)
        frames = bytearray()
        for a, b in zip(left, right):
            frames += struct.pack("<hh", int(a * k * 32767), int(b * k * 32767))
        w.writeframes(bytes(frames))


def buf(seconds):
    return [0.0] * int(seconds * SR)


def env(i, n, attack, release):
    t = i / SR
    a = min(1.0, t / attack) if attack > 0 else 1.0
    r = min(1.0, (n - i) / SR / release) if release > 0 else 1.0
    return a * r


def lowpass(xs, cutoff):
    a = 1 - math.exp(-2 * math.pi * cutoff / SR)
    y, out = 0.0, []
    for x in xs:
        y += a * (x - y)
        out.append(y)
    return out


def highpass(xs, cutoff):
    lp = lowpass(xs, cutoff)
    return [x - l for x, l in zip(xs, lp)]


def add(dst, src, at, gain=1.0):
    o = int(at * SR)
    for i, x in enumerate(src):
        j = o + i
        if 0 <= j < len(dst):
            dst[j] += x * gain


def kick(length=0.42):
    n = int(length * SR)
    out, ph = [], 0.0
    for i in range(n):
        t = i / SR
        f = 48 + 110 * math.exp(-t * 32)
        ph += 2 * math.pi * f / SR
        click = math.exp(-t * 900) * 0.6
        out.append((math.sin(ph) * math.exp(-t * 7.5) + click) * 0.95)
    return out


def hat(length=0.06, open_=False):
    n = int((0.22 if open_ else length) * SR)
    noise = [rng.uniform(-1, 1) for _ in range(n)]
    noise = highpass(noise, 7000)
    decay = 18 if open_ else 70
    return [x * math.exp(-i / SR * decay) * 0.5 for i, x in enumerate(noise)]


def clap():
    n = int(0.25 * SR)
    noise = highpass([rng.uniform(-1, 1) for _ in range(n)], 1200)
    out = []
    for i, x in enumerate(noise):
        t = i / SR
        bursts = sum(math.exp(-max(0.0, t - d) * 140) * (t >= d) for d in (0.0, 0.011, 0.022))
        out.append(x * (bursts * 0.35 + math.exp(-t * 16) * 0.5))
    return out


def saw(f, t):
    return 2 * ((f * t) % 1.0) - 1


def bass_note(freq, length):
    n = int(length * SR)
    raw = [(saw(freq, i / SR) * 0.6 + math.sin(2 * math.pi * freq * i / SR)) * env(i, n, 0.004, 0.06) for i in range(n)]
    return lowpass(raw, 420)


def pad_chord(freqs, length):
    n = int(length * SR)
    left, right = [0.0] * n, [0.0] * n
    for k, f in enumerate(freqs):
        for d, side in ((-0.18, left), (0.18, right)):
            ff = f * (1 + d / 100)
            for i in range(n):
                t = i / SR
                side[i] += (saw(ff, t + k * 0.13) * 0.35 + math.sin(2 * math.pi * ff * t)) * 0.08
    shaped = [env(i, n, 0.35, 0.6) for i in range(n)]
    return lowpass([x * e for x, e in zip(left, shaped)], 1500), lowpass([x * e for x, e in zip(right, shaped)], 1500)


def note(midi):
    return 440.0 * 2 ** ((midi - 69) / 12)


def music():
    total = 15.2
    left, right = buf(total), buf(total)
    k, h, ho, c = kick(), hat(), hat(open_=True), clap()
    beat = 0.5
    drop_at = 12.75
    for b in range(int(drop_at / beat)):
        t = b * beat
        if t >= 0.5 or b == 0:
            add(left, k, t, 0.9)
            add(right, k, t, 0.9)
        if t >= 2.5:
            add(left, h, t + 0.25, 0.32)
            add(right, h, t + 0.25, 0.42)
        if t >= 4.5 and b % 2 == 1:
            add(left, c, t, 0.38)
            add(right, c, t, 0.38)
        if t >= 8.0 and b % 4 == 3:
            add(left, ho, t + 0.25, 0.25)
            add(right, ho, t + 0.25, 0.2)
    # A minor, F, C, G: one chord per bar
    prog = [(57, [57, 60, 64]), (53, [53, 57, 60]), (48, [55, 60, 64]), (55, [55, 59, 62])]
    for bar in range(7):
        t0 = bar * 2.0
        if t0 >= drop_at:
            break
        root, chord = prog[bar % 4]
        pl, pr = pad_chord([note(m + 12) for m in chord], min(2.05, drop_at - t0 + 0.4))
        add(left, pl, t0, 0.9)
        add(right, pr, t0, 0.9)
        if t0 >= 2.0:
            for s in range(8):
                ts = t0 + s * 0.25
                if ts >= drop_at:
                    break
                pattern = [0, 0, 12, 0, 0, 7, 0, 12]
                add(left, bass_note(note(root - 12 + pattern[s]), 0.22), ts, 0.55)
                add(right, bass_note(note(root - 12 + pattern[s]), 0.22), ts, 0.55)
    # logo chord after the impact
    pl, pr = pad_chord([note(m) for m in (69, 72, 76, 81)], 2.2)
    add(left, pl, 13.0, 1.3)
    add(right, pr, 13.0, 1.3)
    fade = int(0.6 * SR)
    for i in range(fade):
        g = i / fade
        left[-1 - i] *= g
        right[-1 - i] *= g
    write("music.wav", left, right)


def whoosh(length=0.55, rising=True):
    n = int(length * SR)
    noise = [rng.uniform(-1, 1) for _ in range(n)]
    out, y = [], 0.0
    for i, x in enumerate(noise):
        p = i / n
        cutoff = 300 + (5200 if rising else 2500) * (math.sin(math.pi * p) ** 2)
        a = 1 - math.exp(-2 * math.pi * cutoff / SR)
        y += a * (x - y)
        out.append(y * math.sin(math.pi * p) ** 1.5)
    return out


def snap():
    n = int(0.28 * SR)
    noise = highpass([rng.uniform(-1, 1) for _ in range(n)], 2500)
    out = []
    for i, x in enumerate(noise):
        t = i / SR
        first = math.exp(-t * 220)
        second = math.exp(-max(0.0, t - 0.055) * 160) * (t >= 0.055) * 0.7
        body = math.sin(2 * math.pi * 1800 * t) * math.exp(-t * 90) * 0.4
        out.append(x * (first + second) + body)
    return out


def click():
    n = int(0.05 * SR)
    return [math.sin(2 * math.pi * 3200 * i / SR) * math.exp(-i / SR * 260) * 0.7 + rng.uniform(-1, 1) * math.exp(-i / SR * 600) * 0.4 for i in range(n)]


def pop(freq=880):
    n = int(0.16 * SR)
    out, ph = [], 0.0
    for i in range(n):
        t = i / SR
        f = freq * (1 + 0.6 * math.exp(-t * 60))
        ph += 2 * math.pi * f / SR
        out.append(math.sin(ph) * math.exp(-t * 26))
    return out


def tick():
    n = int(0.035 * SR)
    return highpass([rng.uniform(-1, 1) * math.exp(-i / SR * 380) for i in range(n)], 3000)


def rec_beep():
    n = int(0.3 * SR)
    return [math.sin(2 * math.pi * 1320 * i / SR) * env(i, n, 0.005, 0.12) * 0.6 for i in range(n)]


def glitch():
    n = int(0.5 * SR)
    out, hold, v = [], 0, 0.0
    for i in range(n):
        if hold <= 0:
            v = rng.choice([-1, 1]) * rng.uniform(0.2, 1.0)
            hold = rng.randint(40, 600)
        hold -= 1
        out.append(v * env(i, n, 0.01, 0.15) * 0.5)
    return lowpass(out, 6000)


def riser(length=0.9):
    n = int(length * SR)
    noise = [rng.uniform(-1, 1) for _ in range(n)]
    out, y, ph = [], 0.0, 0.0
    for i, x in enumerate(noise):
        p = i / n
        a = 1 - math.exp(-2 * math.pi * (400 + 7000 * p * p) / SR)
        y += a * (x - y)
        ph += 2 * math.pi * (300 + 900 * p * p) / SR
        out.append((y * 0.7 + math.sin(ph) * 0.15) * p ** 2)
    return out


def impact():
    n = int(2.0 * SR)
    noise = lowpass([rng.uniform(-1, 1) for _ in range(n)], 900)
    out, ph = [], 0.0
    for i in range(n):
        t = i / SR
        f = 36 + 70 * math.exp(-t * 9)
        ph += 2 * math.pi * f / SR
        out.append(math.sin(ph) * math.exp(-t * 2.2) + noise[i] * math.exp(-t * 7) * 0.6)
    return out


def shimmer():
    n = int(1.6 * SR)
    left, right = [0.0] * n, [0.0] * n
    for k in range(14):
        f = note(rng.choice([93, 96, 100, 105, 108]))
        start = rng.uniform(0, 0.6)
        pan = rng.uniform(0.2, 0.8)
        for i in range(int(start * SR), n):
            t = i / SR - start
            s = math.sin(2 * math.pi * f * t) * math.exp(-t * 5) * 0.12
            left[i] += s * (1 - pan)
            right[i] += s * pan
    return left, right


def main():
    OUT.mkdir(exist_ok=True)
    music()
    write("whoosh.wav", whoosh())
    write("whoosh_short.wav", whoosh(0.32))
    write("snap.wav", snap())
    write("click.wav", click())
    write("pop.wav", pop(880))
    write("pop_hi.wav", pop(1320))
    write("tick.wav", tick())
    write("rec.wav", rec_beep())
    write("glitch.wav", glitch())
    write("riser.wav", riser())
    write("impact.wav", impact())
    sl, sr = shimmer()
    write("shimmer.wav", sl, sr)


if __name__ == "__main__":
    main()
