use fframes::cli::clap;
use fframes::{AudioMixOptions, EncoderOptions, LimiterOptions, RenderOptions, StaticMediaProvider, cli};
use glint_showreel::{ShowreelMedia, ShowreelVideo};
use std::process::ExitCode;

#[derive(Debug, clap::Args)]
struct VideoArgs {}

fn main() -> ExitCode {
    let args = cli::parse::<VideoArgs>();
    let media = ShowreelMedia::prepare().expect("media");
    let video = ShowreelVideo { media: &media };

    cli::new(
        &video,
        RenderOptions {
            media: Some(&media),
            audio_mix: AudioMixOptions {
                limiter: Some(LimiterOptions { ceiling_db: -1.5, ..Default::default() }),
                ..Default::default()
            },
            video_encoder_options: EncoderOptions {
                preferred_encoder: Some("libx264"),
                codec_params: Some(&[("crf", "16"), ("preset", "slow"), ("tune", "animation")]),
                ..Default::default()
            },
            ..Default::default()
        },
    )
    .args(args)
    .run()
}
