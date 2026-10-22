import { useEffect } from 'react';
import { useRoute } from './lib/router';
import { usePrefs } from './lib/prefs';
import { useStore } from './state/store';
import { AppShell } from './components/Shell';
import { PlansScreen } from './screens/PlansScreen';
import { HistoryScreen } from './screens/HistoryScreen';
import { ExercisesScreen } from './screens/ExercisesScreen';
import { ExerciseScreen } from './screens/ExerciseScreen';
import { ProgressScreen } from './screens/ProgressScreen';
import { LoadError, TodayLoading, TodayScreen } from './screens/TodayScreen';

export default function App() {
  const { state } = useStore();
  const route = useRoute();
  const { theme } = usePrefs();
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#101319' : '#eef1f6');
  }, [theme]);
  let screen;
  if (state.load === 'loading') screen = <TodayLoading />;
  else if (state.load === 'error') screen = <LoadError />;
  else
    switch (route.name) {
      case 'plans': screen = <PlansScreen />; break;
      case 'history': screen = <HistoryScreen />; break;
      case 'progress': screen = <ProgressScreen />; break;
      case 'exercises': screen = <ExercisesScreen />; break;
      case 'exercise': screen = <ExerciseScreen id={route.id} />; break;
      default: screen = <TodayScreen />;
    }
  return <AppShell>{screen}</AppShell>;
}
