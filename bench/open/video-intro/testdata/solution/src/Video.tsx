import {AbsoluteFill, Sequence, interpolate, useCurrentFrame} from 'remotion';

const Card: React.FC<{title: string; caption: string; bg: string}> = ({title, caption, bg}) => {
	const f = useCurrentFrame();
	const o = interpolate(f, [0, 12], [0, 1], {extrapolateRight: 'clamp'});
	return (
		<AbsoluteFill style={{backgroundColor: bg, justifyContent: 'center', alignItems: 'center', fontFamily: 'Helvetica, Arial'}}>
			<div style={{opacity: o, textAlign: 'center', color: 'white'}}>
				<div style={{fontSize: 96, fontWeight: 800}}>{title}</div>
				<div style={{fontSize: 40, marginTop: 24}}>{caption}</div>
			</div>
		</AbsoluteFill>
	);
};

const scenes = [
	{title: 'Tidy', caption: 'Clean up messy folders without surprises', bg: '#1d3557'},
	{title: 'tidy scan', caption: 'Finds duplicates and forgotten files', bg: '#457b9d'},
	{title: 'tidy plan', caption: 'Shows every move before it touches anything', bg: '#2a9d8f'},
	{title: 'tidy undo', caption: 'Puts everything back exactly as it was', bg: '#e76f51'},
	{title: 'tidy.dev', caption: 'Free and open source', bg: '#1d3557'},
];

export const Video: React.FC = () => (
	<AbsoluteFill>
		{scenes.map((s, i) => (
			<Sequence key={s.title} from={i * 120} durationInFrames={120}>
				<Card {...s} />
			</Sequence>
		))}
	</AbsoluteFill>
);
