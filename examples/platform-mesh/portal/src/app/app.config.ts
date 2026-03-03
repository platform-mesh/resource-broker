import { ApplicationConfig } from '@angular/core';
import { provideRouter, withHashLocation } from '@angular/router';
import {
  LuigiContextService,
  LuigiContextServiceImpl,
} from '@luigi-project/client-support-angular';
import { routes } from './app.routes';

export const appConfig: ApplicationConfig = {
  providers: [
    provideRouter(routes, withHashLocation()),
    {
      provide: LuigiContextService,
      useClass: LuigiContextServiceImpl,
    },
  ],
};
