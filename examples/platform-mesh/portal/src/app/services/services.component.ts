/**
 * Services Component
 *
 * Main layout with side navigation for service categories (Compute, PKI).
 * Shows available resource types organized by category in a side menu.
 */
import { Component, CUSTOM_ELEMENTS_SCHEMA, inject, signal, OnInit } from '@angular/core';
import { Router, RouterOutlet, ActivatedRoute } from '@angular/router';
import LuigiClient from '@luigi-project/client';
import {
  IconComponent,
  LabelComponent,
  TextComponent,
  TitleComponent,
} from '@ui5/webcomponents-ngx';

import '@ui5/webcomponents-icons/dist/it-host.js';
import '@ui5/webcomponents-icons/dist/locked.js';
import '@ui5/webcomponents-icons/dist/navigation-right-arrow.js';
import '@ui5/webcomponents-icons/dist/navigation-down-arrow.js';
import '@ui5/webcomponents-icons/dist/customer.js';
import '@ui5/webcomponents-icons/dist/home.js';

import { GenericResourceService, ServiceCategory, DiscoveredResource } from './generic-resource.service';

@Component({
  selector: 'app-services',
  standalone: true,
  imports: [
    RouterOutlet,
    TitleComponent,
    LabelComponent,
    TextComponent,
    IconComponent,
  ],
  schemas: [CUSTOM_ELEMENTS_SCHEMA],
  templateUrl: './services.component.html',
  styleUrl: './services.component.scss',
})
export class ServicesComponent implements OnInit {
  private router = inject(Router);
  private route = inject(ActivatedRoute);
  private genericResourceService = inject(GenericResourceService);

  public categories = signal<ServiceCategory[]>([]);
  public loading = signal<boolean>(true);
  public selectedResource = signal<DiscoveredResource | null>(null);
  public expandedCategories = signal<Set<string>>(new Set(['Compute', 'PKI']));

  ngOnInit(): void {
    LuigiClient.addInitListener(() => {
      LuigiClient.uxManager().showLoadingIndicator();
      this.loadCategories();
      LuigiClient.uxManager().hideLoadingIndicator();
    });
  }

  private loadCategories(): void {
    const categories = this.genericResourceService.getServiceCategories();
    this.categories.set(categories);
    this.loading.set(false);

    // Auto-select first resource if none selected
    if (categories.length > 0 && categories[0].resources.length > 0) {
      const firstResource = categories[0].resources[0];
      this.selectResource(firstResource);
    }
  }

  public selectResource(resource: DiscoveredResource): void {
    this.selectedResource.set(resource);
    this.router.navigate([resource.group, resource.kind.toLowerCase()], { relativeTo: this.route });
  }

  public isResourceSelected(resource: DiscoveredResource): boolean {
    const selected = this.selectedResource();
    return selected?.group === resource.group && selected?.kind === resource.kind;
  }

  public toggleCategory(categoryName: string): void {
    const expanded = new Set(this.expandedCategories());
    if (expanded.has(categoryName)) {
      expanded.delete(categoryName);
    } else {
      expanded.add(categoryName);
    }
    this.expandedCategories.set(expanded);
  }

  public isCategoryExpanded(categoryName: string): boolean {
    return this.expandedCategories().has(categoryName);
  }
}
