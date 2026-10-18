import {StrictMode} from 'react';
import {createRoot} from 'react-dom/client';
import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
import App from './App';
import '../../web/style.css';
const client=new QueryClient({defaultOptions:{queries:{retry:false,staleTime:1000}}});
createRoot(document.getElementById('root')!).render(<StrictMode><QueryClientProvider client={client}><App/></QueryClientProvider></StrictMode>);
