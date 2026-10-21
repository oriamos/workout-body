import { useRoute } from './lib/router';
import { useStore } from './state/store';
import { AppShell } from './components/Shell';
import { PlansScreen } from './screens/PlansScreen';
import { HistoryScreen } from './screens/HistoryScreen';
import { ExerciseScreen } from './screens/ExerciseScreen';
import { ProgressScreen } from './screens/ProgressScreen';
import { LoadError, TodayLoading, TodayScreen } from './screens/TodayScreen';

export default function App() {
  const { state } = useStore();
  const route = useRoute();
  let screen;
  if (state.load === 'loading') screen = <TodayLoading />;
  else if (state.load === 'error') screen = <LoadError />;
  else
    switch (route.name) {
      case 'plans': screen = <PlansScreen />; break;
      case 'history': screen = <HistoryScreen />; break;
      case 'progress': screen = <ProgressScreen />; break;
      case 'exercise': screen = <ExerciseScreen id={route.id} />; break;
      default: screen = <TodayScreen />;
    }
  return <AppShell>{screen}</AppShell>;
}
