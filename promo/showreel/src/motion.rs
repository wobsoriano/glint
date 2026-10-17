//! Easing curves and small numeric helpers shared by every shot.

pub fn c01(x: f32) -> f32 {
    x.clamp(0.0, 1.0)
}

/// 0..1 over `d` seconds from `a`.
pub fn seg(t: f32, a: f32, d: f32) -> f32 {
    c01((t - a) / d)
}

pub fn lerp(a: f32, b: f32, p: f32) -> f32 {
    a + (b - a) * p
}

pub fn out_expo(x: f32) -> f32 {
    if x >= 1.0 {
        1.0
    } else {
        1.0 - 2f32.powf(-10.0 * x)
    }
}

pub fn out_cubic(x: f32) -> f32 {
    1.0 - (1.0 - x).powi(3)
}

pub fn in_cubic(x: f32) -> f32 {
    x * x * x
}

pub fn in_out(x: f32) -> f32 {
    if x < 0.5 {
        4.0 * x * x * x
    } else {
        1.0 - (-2.0 * x + 2.0).powi(3) / 2.0
    }
}

pub fn out_back(x: f32) -> f32 {
    let c1 = 1.70158;
    let c3 = c1 + 1.0;
    1.0 + c3 * (x - 1.0).powi(3) + c1 * (x - 1.0).powi(2)
}

/// Damped spring from 0 to 1, `t` seconds after it starts (mass 1).
pub fn spring(t: f32, stiffness: f32, damping: f32) -> f32 {
    if t <= 0.0 {
        return 0.0;
    }
    let w0 = stiffness.sqrt();
    let z = damping / (2.0 * w0);
    if z < 1.0 {
        let wd = w0 * (1.0 - z * z).sqrt();
        1.0 - (-z * w0 * t).exp() * ((wd * t).cos() + z * w0 / wd * (wd * t).sin())
    } else {
        1.0 - (-w0 * t).exp() * (1.0 + w0 * t)
    }
}

pub fn hash(i: u32) -> f32 {
    let mut x = i.wrapping_mul(2_654_435_761) ^ 0x9e37_79b9;
    x ^= x >> 15;
    x = x.wrapping_mul(2_246_822_519);
    x ^= x >> 13;
    (x & 0xffff) as f32 / 65535.0
}
