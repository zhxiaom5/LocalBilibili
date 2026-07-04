import '@testing-library/jest-dom/vitest';
import { vi } from 'vitest';

Object.defineProperty(HTMLMediaElement.prototype, 'load', {
  configurable: true,
  value: vi.fn(),
});
