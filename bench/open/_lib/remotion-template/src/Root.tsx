import {Composition} from 'remotion';
import {Video} from './Video';

// Render with: npx remotion render src/index.ts Video out/video.mp4
export const RemotionRoot: React.FC = () => {
	return <Composition id="Video" component={Video} durationInFrames={30} fps={30} width={1280} height={720} />;
};
