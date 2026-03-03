import { Routes } from '@angular/router';
import { ServicesComponent } from './services/services.component';
import { ResourceListComponent } from './services/resource-list/resource-list.component';

export const routes: Routes = [
  { path: '', redirectTo: 'services', pathMatch: 'full' },
  {
    path: 'services',
    component: ServicesComponent,
    children: [
      { path: ':group/:kind', component: ResourceListComponent },
    ],
  },
  { path: '**', redirectTo: 'services' },
];
