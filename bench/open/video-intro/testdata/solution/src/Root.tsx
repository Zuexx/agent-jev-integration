import {Composition} from 'remotion';
import {Video} from './Video';

// Render with: npx remotion render src/index.ts Video out/video.mp4
export const RemotionRoot: React.FC = () => {
	return <Composition id="Intro" component={Video} durationInFrames={600} fps={30} width={1280} height={720} />;
};
