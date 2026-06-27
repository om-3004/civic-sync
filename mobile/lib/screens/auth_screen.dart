// Auth screen stub — placeholder for task 14.2.
//
// This file satisfies the routing requirement from main.dart and will be
// fully implemented in task 14.2 with:
//   - GoogleSignIn().signIn() call
//   - ID token extraction and POST /auth/login to the backend
//   - Role-based navigation to CitizenFeedScreen or OfficialDashboardScreen
//
// Requirements: 1.1, 1.2, 1.3

import 'package:flutter/material.dart';

/// Placeholder authentication screen.
///
/// Displays a "Sign in with Google" button stub; actual OAuth logic is
/// implemented in task 14.2.
class AuthScreen extends StatelessWidget {
  const AuthScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: const Color(0xFFF8F9FA),
      body: SafeArea(
        child: Center(
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 32.0),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                // App logo / wordmark placeholder
                const Icon(
                  Icons.location_city,
                  size: 72,
                  color: Color(0xFF1A73E8),
                ),
                const SizedBox(height: 24),
                Text(
                  'CivicSync',
                  style: Theme.of(context).textTheme.headlineLarge?.copyWith(
                        fontWeight: FontWeight.bold,
                        color: const Color(0xFF202124),
                      ),
                ),
                const SizedBox(height: 8),
                Text(
                  'Report civic issues in your community',
                  textAlign: TextAlign.center,
                  style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                        color: const Color(0xFF5F6368),
                      ),
                ),
                const SizedBox(height: 48),
                // Sign-in button — TODO: wire to GoogleSignIn in task 14.2
                ElevatedButton.icon(
                  onPressed: () {
                    // TODO (task 14.2): call GoogleSignIn().signIn(), extract
                    // ID token, POST to /auth/login, navigate by role.
                    ScaffoldMessenger.of(context).showSnackBar(
                      const SnackBar(
                        content: Text('Google Sign-In — implemented in task 14.2'),
                      ),
                    );
                  },
                  icon: const Icon(Icons.login),
                  label: const Text('Sign in with Google'),
                  style: ElevatedButton.styleFrom(
                    minimumSize: const Size.fromHeight(50),
                    backgroundColor: const Color(0xFF1A73E8),
                    foregroundColor: Colors.white,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
