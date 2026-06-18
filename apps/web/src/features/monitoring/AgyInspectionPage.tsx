import type { AntigravityTargetProvider } from '@/services/api/antigravityInspectionService';
import { AgyAccountStatusPage } from './AgyAccountStatusPage';

type AgyInspectionPageProps = {
  provider: Extract<AntigravityTargetProvider, 'claude' | 'gemini'>;
};

export function AgyInspectionPage({ provider }: AgyInspectionPageProps) {
  return <AgyAccountStatusPage provider={provider} />;
}
